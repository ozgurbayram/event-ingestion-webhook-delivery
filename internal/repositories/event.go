package repositories

import (
	"context"
	"errors"

	"event-intestion/internal/entities"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrEventNotFound = errors.New("event not found")
var ErrDuplicateEvent = errors.New("duplicate event")

type EventRepository interface {
	Create(ctx context.Context, tx *gorm.DB, event *entities.Event) error
	GetByID(ctx context.Context, id uuid.UUID) (*entities.Event, error)
	ExistsByIdempotencyKey(ctx context.Context, applicationID, idempotencyKey string) (bool, error)
}

type eventRepository struct {
	db *gorm.DB
}

func NewEventRepository(db *gorm.DB) EventRepository {
	return &eventRepository{db: db}
}

func (r *eventRepository) Create(ctx context.Context, tx *gorm.DB, event *entities.Event) error {
	db := r.db
	if tx != nil {
		db = tx
	}

	result := db.WithContext(ctx).Create(event)
	if result.Error != nil {
		if isUniqueViolation(result.Error) {
			return ErrDuplicateEvent
		}
		return result.Error
	}
	return nil
}

func (r *eventRepository) GetByID(ctx context.Context, id uuid.UUID) (*entities.Event, error) {
	var event entities.Event
	result := r.db.WithContext(ctx).Where("id = ?", id).First(&event)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrEventNotFound
		}
		return nil, result.Error
	}
	return &event, nil
}

func (r *eventRepository) ExistsByIdempotencyKey(ctx context.Context, applicationID, idempotencyKey string) (bool, error) {
	var count int64
	result := r.db.WithContext(ctx).
		Model(&entities.Event{}).
		Where("application_id = ? AND idempotency_key = ?", applicationID, idempotencyKey).
		Count(&count)
	if result.Error != nil {
		return false, result.Error
	}
	return count > 0, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && (errors.Is(err, gorm.ErrDuplicatedKey) ||
		containsString(err.Error(), "duplicate key") ||
		containsString(err.Error(), "unique constraint"))
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
