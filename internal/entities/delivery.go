package entities

import (
	"time"

	"github.com/google/uuid"
)

type DeliveryStatus string

const (
	DeliveryStatusPending    DeliveryStatus = "pending"
	DeliveryStatusInProgress DeliveryStatus = "in_progress"
	DeliveryStatusSuccess    DeliveryStatus = "success"
	DeliveryStatusFailed     DeliveryStatus = "failed"
	DeliveryStatusExhausted  DeliveryStatus = "exhausted"
)

type Delivery struct {
	ID            uuid.UUID      `gorm:"type:uuid;primaryKey"`
	EventID       uuid.UUID      `gorm:"type:uuid;not null;index:idx_delivery_event"`
	WebhookID     uuid.UUID      `gorm:"type:uuid;not null"`
	Status        DeliveryStatus `gorm:"type:varchar(50);not null;default:'pending';index:idx_delivery_status_retry"`
	AttemptCount  int            `gorm:"default:0"`
	NextRetryAt   time.Time      `gorm:"index:idx_delivery_status_retry"`
	LastError     string         `gorm:"type:text"`
	LastAttemptAt *time.Time
	CreatedAt     time.Time `gorm:"autoCreateTime"`

	Event   *Event   `gorm:"foreignKey:EventID"`
	Webhook *Webhook `gorm:"foreignKey:WebhookID"`
}

func (Delivery) TableName() string {
	return "deliveries"
}

func NewDelivery(eventID, webhookID uuid.UUID) *Delivery {
	return &Delivery{
		ID:           uuid.New(),
		EventID:      eventID,
		WebhookID:    webhookID,
		Status:       DeliveryStatusPending,
		AttemptCount: 0,
		NextRetryAt:  time.Now(),
	}
}

func (d *Delivery) MarkInProgress() {
	d.Status = DeliveryStatusInProgress
}

func (d *Delivery) MarkSuccess() {
	now := time.Now()
	d.Status = DeliveryStatusSuccess
	d.LastAttemptAt = &now
	d.AttemptCount++
}

func (d *Delivery) MarkFailed(err string, nextRetryAt time.Time, maxRetries int) {
	now := time.Now()
	d.AttemptCount++
	d.LastAttemptAt = &now
	d.LastError = err

	if d.AttemptCount >= maxRetries {
		d.Status = DeliveryStatusExhausted
	} else {
		d.Status = DeliveryStatusPending
		d.NextRetryAt = nextRetryAt
	}
}
