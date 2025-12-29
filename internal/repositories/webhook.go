package repositories

import (
	"context"
	"errors"

	"event-intestion/internal/entities"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrWebhookNotFound = errors.New("webhook not found")

type WebhookRepository interface {
	Create(ctx context.Context, webhook *entities.Webhook) error
	GetByID(ctx context.Context, id uuid.UUID) (*entities.Webhook, error)
	GetActiveByApplicationID(ctx context.Context, applicationID string) ([]*entities.Webhook, error)
	Update(ctx context.Context, webhook *entities.Webhook) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type webhookRepository struct {
	db *gorm.DB
}

func NewWebhookRepository(db *gorm.DB) WebhookRepository {
	return &webhookRepository{db: db}
}

func (r *webhookRepository) Create(ctx context.Context, webhook *entities.Webhook) error {
	return r.db.WithContext(ctx).Create(webhook).Error
}

func (r *webhookRepository) GetByID(ctx context.Context, id uuid.UUID) (*entities.Webhook, error) {
	var webhook entities.Webhook
	result := r.db.WithContext(ctx).Where("id = ?", id).First(&webhook)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrWebhookNotFound
		}
		return nil, result.Error
	}
	return &webhook, nil
}

func (r *webhookRepository) GetActiveByApplicationID(ctx context.Context, applicationID string) ([]*entities.Webhook, error) {
	var webhooks []*entities.Webhook
	result := r.db.WithContext(ctx).
		Where("application_id = ? AND active = ?", applicationID, true).
		Find(&webhooks)
	if result.Error != nil {
		return nil, result.Error
	}
	return webhooks, nil
}

func (r *webhookRepository) Update(ctx context.Context, webhook *entities.Webhook) error {
	return r.db.WithContext(ctx).Save(webhook).Error
}

func (r *webhookRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&entities.Webhook{}, id).Error
}
