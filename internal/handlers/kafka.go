package handlers

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"event-intestion/internal/entities"
	"event-intestion/internal/ingestor"

	"github.com/IBM/sarama"
)

type KafkaHandler struct {
	ingestorService ingestor.Service
}

func NewKafkaHandler(ingestorService ingestor.Service) *KafkaHandler {
	return &KafkaHandler{
		ingestorService: ingestorService,
	}
}

type KafkaEventMessage struct {
	ApplicationID  string                 `json:"application_id"`
	EventType      string                 `json:"event_type"`
	IdempotencyKey string                 `json:"idempotency_key"`
	Payload        map[string]interface{} `json:"payload"`
	OccurredAt     *time.Time             `json:"occurred_at,omitempty"`
}

func (h *KafkaHandler) Setup(sarama.ConsumerGroupSession) error {
	return nil
}

func (h *KafkaHandler) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

func (h *KafkaHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		h.processMessage(session.Context(), msg)
		session.MarkMessage(msg, "")
	}
	return nil
}

func (h *KafkaHandler) processMessage(ctx context.Context, msg *sarama.ConsumerMessage) {
	var event KafkaEventMessage
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		log.Printf("failed to unmarshal kafka message: %v", err)
		return
	}

	if event.ApplicationID == "" || event.EventType == "" || event.IdempotencyKey == "" {
		log.Printf("invalid kafka message: missing required fields")
		return
	}

	occurredAt := time.Now()
	if event.OccurredAt != nil {
		occurredAt = *event.OccurredAt
	}

	resp, err := h.ingestorService.Ingest(ctx, ingestor.IngestRequest{
		ApplicationID:  event.ApplicationID,
		EventType:      event.EventType,
		IdempotencyKey: event.IdempotencyKey,
		Payload:        event.Payload,
		Source:         entities.EventSourceKafka,
		OccurredAt:     occurredAt,
	})

	if err != nil {
		log.Printf("failed to ingest event from kafka: %v", err)
		return
	}

	if resp.AlreadyProcessed {
		log.Printf("event already processed: %s", event.IdempotencyKey)
		return
	}

	log.Printf("ingested event %s with %d deliveries", resp.EventID, resp.DeliveriesCount)
}

type KafkaConsumer struct {
	client  sarama.ConsumerGroup
	handler *KafkaHandler
	topic   string
}

func NewKafkaConsumer(brokers []string, groupID, topic string, handler *KafkaHandler) (*KafkaConsumer, error) {
	config := sarama.NewConfig()
	config.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	config.Consumer.Offsets.Initial = sarama.OffsetNewest

	client, err := sarama.NewConsumerGroup(brokers, groupID, config)
	if err != nil {
		return nil, err
	}

	return &KafkaConsumer{
		client:  client,
		handler: handler,
		topic:   topic,
	}, nil
}

func (c *KafkaConsumer) Start(ctx context.Context) error {
	for {
		if err := c.client.Consume(ctx, []string{c.topic}, c.handler); err != nil {
			log.Printf("kafka consumer error: %v", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (c *KafkaConsumer) Close() error {
	return c.client.Close()
}
