package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"event-intestion/internal/config"
	"event-intestion/internal/db"
	"event-intestion/internal/handlers"
	"event-intestion/internal/ingestor"
	"event-intestion/internal/repositories"
	"event-intestion/internal/worker"

	"github.com/gorilla/mux"
)

func main() {
	cfg := config.Load()

	database, err := db.NewPostgresConnection(cfg.Database)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	if err := db.Migrate(database); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	eventRepo := repositories.NewEventRepository(database)
	webhookRepo := repositories.NewWebhookRepository(database)
	deliveryRepo := repositories.NewDeliveryRepository(database)

	ingestorService := ingestor.NewService(database, eventRepo, webhookRepo, deliveryRepo)

	restHandler := handlers.NewRESTHandler(ingestorService, webhookRepo)

	router := mux.NewRouter()
	restHandler.RegisterRoutes(router)

	server := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      handlers.CORSMiddleware(router),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	dispatcher := worker.NewDispatcher(cfg.Worker.RequestTimeout)
	poller := worker.NewPoller(
		deliveryRepo,
		dispatcher,
		cfg.Worker.PollingInterval,
		cfg.Worker.BatchSize,
		cfg.Worker.MaxRetries,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poller.Start(ctx)

	kafkaHandler := handlers.NewKafkaHandler(ingestorService)
	kafkaConsumer, err := handlers.NewKafkaConsumer(
		cfg.Kafka.Brokers,
		cfg.Kafka.GroupID,
		cfg.Kafka.Topic,
		kafkaHandler,
	)
	if err != nil {
		log.Printf("failed to create kafka consumer: %v", err)
	} else {
		go func() {
			if err := kafkaConsumer.Start(ctx); err != nil {
				log.Printf("kafka consumer stopped: %v", err)
			}
		}()
	}

	go func() {
		log.Printf("starting server on port %s", cfg.Server.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")

	cancel()

	poller.Stop()

	if kafkaConsumer != nil {
		kafkaConsumer.Close()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown error: %v", err)
	}

	log.Println("shutdown complete")
}
