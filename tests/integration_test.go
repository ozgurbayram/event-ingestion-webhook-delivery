package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"event-intestion/internal/config"
	"event-intestion/internal/db"
	"event-intestion/internal/entities"
	"event-intestion/internal/handlers"
	"event-intestion/internal/ingestor"
	"event-intestion/internal/repositories"

	"github.com/gorilla/mux"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type testServer struct {
	router       *mux.Router
	db           *gorm.DB
	eventRepo    repositories.EventRepository
	webhookRepo  repositories.WebhookRepository
	deliveryRepo repositories.DeliveryRepository
}

func setupTestServer(t *testing.T) *testServer {
	cfg := config.Load()

	database, err := gorm.Open(postgres.Open(cfg.Database.DSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}

	if err := db.Migrate(database); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	cleanupDatabase(database)

	eventRepo := repositories.NewEventRepository(database)
	webhookRepo := repositories.NewWebhookRepository(database)
	deliveryRepo := repositories.NewDeliveryRepository(database)

	ingestorService := ingestor.NewService(database, eventRepo, webhookRepo, deliveryRepo)
	restHandler := handlers.NewRESTHandler(ingestorService, webhookRepo)

	router := mux.NewRouter()
	restHandler.RegisterRoutes(router)

	return &testServer{
		router:       router,
		db:           database,
		eventRepo:    eventRepo,
		webhookRepo:  webhookRepo,
		deliveryRepo: deliveryRepo,
	}
}

func cleanupDatabase(db *gorm.DB) {
	db.Exec("DELETE FROM deliveries")
	db.Exec("DELETE FROM events")
	db.Exec("DELETE FROM webhooks")
}

func TestHealthCheck(t *testing.T) {
	ts := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	ts.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got '%s'", resp["status"])
	}
}

func TestCreateWebhook(t *testing.T) {
	ts := setupTestServer(t)

	payload := map[string]interface{}{
		"application_id": "app-1",
		"url":            "http://webhook-receiver:8080/webhook",
		"secret":         "test-secret",
		"event_types":    []string{"order.created", "order.updated"},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/webhooks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ts.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var resp handlers.WebhookResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.ApplicationID != "app-1" {
		t.Errorf("expected application_id 'app-1', got '%s'", resp.ApplicationID)
	}

	if !resp.Active {
		t.Error("expected webhook to be active")
	}
}

func TestGetWebhook(t *testing.T) {
	ts := setupTestServer(t)

	webhook := entities.NewWebhook("app-1", "http://example.com/webhook", "secret", []string{"*"})
	ts.webhookRepo.Create(context.Background(), webhook)

	req := httptest.NewRequest(http.MethodGet, "/webhooks/"+webhook.ID.String(), nil)
	rec := httptest.NewRecorder()
	ts.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp handlers.WebhookResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.ID != webhook.ID.String() {
		t.Errorf("expected id '%s', got '%s'", webhook.ID.String(), resp.ID)
	}
}

func TestUpdateWebhook(t *testing.T) {
	ts := setupTestServer(t)

	webhook := entities.NewWebhook("app-1", "http://example.com/webhook", "secret", []string{"*"})
	ts.webhookRepo.Create(context.Background(), webhook)

	updatePayload := map[string]interface{}{
		"url":    "http://updated.com/webhook",
		"active": false,
	}

	body, _ := json.Marshal(updatePayload)
	req := httptest.NewRequest(http.MethodPut, "/webhooks/"+webhook.ID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ts.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp handlers.WebhookResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.URL != "http://updated.com/webhook" {
		t.Errorf("expected url 'http://updated.com/webhook', got '%s'", resp.URL)
	}

	if resp.Active {
		t.Error("expected webhook to be inactive")
	}
}

func TestDeleteWebhook(t *testing.T) {
	ts := setupTestServer(t)

	webhook := entities.NewWebhook("app-1", "http://example.com/webhook", "secret", []string{"*"})
	ts.webhookRepo.Create(context.Background(), webhook)

	req := httptest.NewRequest(http.MethodDelete, "/webhooks/"+webhook.ID.String(), nil)
	rec := httptest.NewRecorder()
	ts.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
}

func TestIngestEvent(t *testing.T) {
	ts := setupTestServer(t)

	payload := map[string]interface{}{
		"application_id":  "app-1",
		"event_type":      "order.created",
		"idempotency_key": "order-123",
		"payload": map[string]interface{}{
			"order_id": "123",
			"amount":   99.99,
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ts.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var resp handlers.IngestEventResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Status != "created" {
		t.Errorf("expected status 'created', got '%s'", resp.Status)
	}

	if resp.EventID == "" {
		t.Error("expected event_id to be set")
	}
}

func TestIngestEventIdempotency(t *testing.T) {
	ts := setupTestServer(t)

	payload := map[string]interface{}{
		"application_id":  "app-1",
		"event_type":      "order.created",
		"idempotency_key": "order-456",
		"payload": map[string]interface{}{
			"order_id": "456",
		},
	}

	body, _ := json.Marshal(payload)

	req1 := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	ts.router.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusCreated {
		t.Errorf("first request: expected status %d, got %d", http.StatusCreated, rec1.Code)
	}

	body, _ = json.Marshal(payload)
	req2 := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	ts.router.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("second request: expected status %d, got %d", http.StatusOK, rec2.Code)
	}

	var resp handlers.IngestEventResponse
	json.NewDecoder(rec2.Body).Decode(&resp)

	if resp.Status != "already_processed" {
		t.Errorf("expected status 'already_processed', got '%s'", resp.Status)
	}
}

func TestIngestEventCreatesDeliveries(t *testing.T) {
	ts := setupTestServer(t)

	webhook := entities.NewWebhook("app-1", "http://example.com/webhook", "secret", []string{"order.created"})
	ts.webhookRepo.Create(context.Background(), webhook)

	payload := map[string]interface{}{
		"application_id":  "app-1",
		"event_type":      "order.created",
		"idempotency_key": "order-789",
		"payload": map[string]interface{}{
			"order_id": "789",
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ts.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var resp handlers.IngestEventResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.DeliveriesCount != 1 {
		t.Errorf("expected 1 delivery, got %d", resp.DeliveriesCount)
	}
}

func TestIngestEventNoMatchingWebhook(t *testing.T) {
	ts := setupTestServer(t)

	webhook := entities.NewWebhook("app-1", "http://example.com/webhook", "secret", []string{"payment.completed"})
	ts.webhookRepo.Create(context.Background(), webhook)

	payload := map[string]interface{}{
		"application_id":  "app-1",
		"event_type":      "order.created",
		"idempotency_key": "order-no-match",
		"payload": map[string]interface{}{
			"order_id": "999",
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ts.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, rec.Code)
	}

	var resp handlers.IngestEventResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.DeliveriesCount != 0 {
		t.Errorf("expected 0 deliveries, got %d", resp.DeliveriesCount)
	}
}

func TestIngestEventWildcardWebhook(t *testing.T) {
	ts := setupTestServer(t)

	webhook := entities.NewWebhook("app-1", "http://example.com/webhook", "secret", []string{"*"})
	ts.webhookRepo.Create(context.Background(), webhook)

	payload := map[string]interface{}{
		"application_id":  "app-1",
		"event_type":      "any.event.type",
		"idempotency_key": "wildcard-test",
		"payload":         map[string]interface{}{},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ts.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, rec.Code)
	}

	var resp handlers.IngestEventResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.DeliveriesCount != 1 {
		t.Errorf("expected 1 delivery for wildcard webhook, got %d", resp.DeliveriesCount)
	}
}

func TestIngestEventValidation(t *testing.T) {
	ts := setupTestServer(t)

	tests := []struct {
		name    string
		payload map[string]interface{}
	}{
		{
			name: "missing application_id",
			payload: map[string]interface{}{
				"event_type":      "order.created",
				"idempotency_key": "key-1",
			},
		},
		{
			name: "missing event_type",
			payload: map[string]interface{}{
				"application_id":  "app-1",
				"idempotency_key": "key-2",
			},
		},
		{
			name: "missing idempotency_key",
			payload: map[string]interface{}{
				"application_id": "app-1",
				"event_type":     "order.created",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.payload)
			req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			ts.router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
			}
		})
	}
}

func TestMultipleWebhooksReceiveDeliveries(t *testing.T) {
	ts := setupTestServer(t)

	webhook1 := entities.NewWebhook("app-1", "http://example.com/webhook1", "secret1", []string{"order.created"})
	webhook2 := entities.NewWebhook("app-1", "http://example.com/webhook2", "secret2", []string{"order.created"})
	webhook3 := entities.NewWebhook("app-1", "http://example.com/webhook3", "secret3", []string{"payment.completed"})

	ts.webhookRepo.Create(context.Background(), webhook1)
	ts.webhookRepo.Create(context.Background(), webhook2)
	ts.webhookRepo.Create(context.Background(), webhook3)

	payload := map[string]interface{}{
		"application_id":  "app-1",
		"event_type":      "order.created",
		"idempotency_key": fmt.Sprintf("multi-webhook-%d", time.Now().UnixNano()),
		"payload":         map[string]interface{}{},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ts.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, rec.Code)
	}

	var resp handlers.IngestEventResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.DeliveriesCount != 2 {
		t.Errorf("expected 2 deliveries (webhook1 and webhook2), got %d", resp.DeliveriesCount)
	}
}
