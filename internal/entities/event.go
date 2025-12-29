package entities

import (
	"time"

	"github.com/google/uuid"
)

type EventSource string

const (
	EventSourceREST  EventSource = "rest"
	EventSourceKafka EventSource = "kafka"
)

func (s EventSource) IsValid() bool {
	switch s {
	case EventSourceREST, EventSourceKafka:
		return true
	default:
		return false
	}
}

type Event struct {
	ID             uuid.UUID              `gorm:"type:uuid;primaryKey"`
	ApplicationID  string                 `gorm:"type:varchar(255);not null;index:idx_app_idempotency,unique"`
	EventType      string                 `gorm:"type:varchar(255);not null;index:idx_app_event_type"`
	IdempotencyKey string                 `gorm:"type:varchar(255);not null;index:idx_app_idempotency,unique"`
	Payload        map[string]interface{} `gorm:"type:jsonb;not null"`
	Source         EventSource            `gorm:"type:varchar(50);not null"`
	OccurredAt     time.Time              `gorm:"not null"`
	CreatedAt      time.Time              `gorm:"autoCreateTime"`
}

func (Event) TableName() string {
	return "events"
}

func NewEvent(applicationID, eventType, idempotencyKey string, payload map[string]interface{}, source EventSource, occurredAt time.Time) *Event {
	return &Event{
		ID:             uuid.New(),
		ApplicationID:  applicationID,
		EventType:      eventType,
		IdempotencyKey: idempotencyKey,
		Payload:        payload,
		Source:         source,
		OccurredAt:     occurredAt,
	}
}
