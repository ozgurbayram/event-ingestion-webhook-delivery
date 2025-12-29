package entities

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type Webhook struct {
	ID            uuid.UUID      `gorm:"type:uuid;primaryKey"`
	ApplicationID string         `gorm:"type:varchar(255);not null;index:idx_webhook_app_active"`
	URL           string         `gorm:"type:varchar(2048);not null"`
	Secret        string         `gorm:"type:varchar(255);not null"`
	EventTypes    pq.StringArray `gorm:"type:text[];not null"`
	Active        bool           `gorm:"default:true;index:idx_webhook_app_active"`
	CreatedAt     time.Time      `gorm:"autoCreateTime"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime"`
}

func (Webhook) TableName() string {
	return "webhooks"
}

func NewWebhook(applicationID, url, secret string, eventTypes []string) *Webhook {
	return &Webhook{
		ID:            uuid.New(),
		ApplicationID: applicationID,
		URL:           url,
		Secret:        secret,
		EventTypes:    eventTypes,
		Active:        true,
	}
}

func (w *Webhook) SubscribesToEventType(eventType string) bool {
	for _, et := range w.EventTypes {
		if et == eventType || et == "*" {
			return true
		}
	}
	return false
}
