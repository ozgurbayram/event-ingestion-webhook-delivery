package repositories

import (
	"context"
	"errors"
	"time"

	"event-intestion/internal/entities"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrDeliveryNotFound = errors.New("delivery not found")

type DeliveryRepository interface {
	Create(ctx context.Context, tx *gorm.DB, delivery *entities.Delivery) error
	CreateBatch(ctx context.Context, tx *gorm.DB, deliveries []*entities.Delivery) error
	GetByID(ctx context.Context, id uuid.UUID) (*entities.Delivery, error)
	GetByEventID(ctx context.Context, eventID uuid.UUID) ([]*entities.Delivery, error)
	FetchPendingForProcessing(ctx context.Context, limit int) ([]*entities.Delivery, error)
	Update(ctx context.Context, delivery *entities.Delivery) error
}

type deliveryRepository struct {
	db *gorm.DB
}

func NewDeliveryRepository(db *gorm.DB) DeliveryRepository {
	return &deliveryRepository{db: db}
}

func (r *deliveryRepository) Create(ctx context.Context, tx *gorm.DB, delivery *entities.Delivery) error {
	db := r.db
	if tx != nil {
		db = tx
	}
	return db.WithContext(ctx).Create(delivery).Error
}

func (r *deliveryRepository) CreateBatch(ctx context.Context, tx *gorm.DB, deliveries []*entities.Delivery) error {
	if len(deliveries) == 0 {
		return nil
	}
	db := r.db
	if tx != nil {
		db = tx
	}
	return db.WithContext(ctx).Create(&deliveries).Error
}

func (r *deliveryRepository) GetByID(ctx context.Context, id uuid.UUID) (*entities.Delivery, error) {
	var delivery entities.Delivery
	result := r.db.WithContext(ctx).
		Preload("Event").
		Preload("Webhook").
		Where("id = ?", id).
		First(&delivery)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrDeliveryNotFound
		}
		return nil, result.Error
	}
	return &delivery, nil
}

func (r *deliveryRepository) GetByEventID(ctx context.Context, eventID uuid.UUID) ([]*entities.Delivery, error) {
	var deliveries []*entities.Delivery
	result := r.db.WithContext(ctx).
		Where("event_id = ?", eventID).
		Find(&deliveries)
	if result.Error != nil {
		return nil, result.Error
	}
	return deliveries, nil
}

func (r *deliveryRepository) FetchPendingForProcessing(ctx context.Context, limit int) ([]*entities.Delivery, error) {
	var deliveries []*entities.Delivery
	result := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Preload("Event").
		Preload("Webhook").
		Where("status = ? AND next_retry_at <= ?", entities.DeliveryStatusPending, time.Now()).
		Order("next_retry_at ASC").
		Limit(limit).
		Find(&deliveries)
	if result.Error != nil {
		return nil, result.Error
	}

	if len(deliveries) > 0 {
		ids := make([]uuid.UUID, len(deliveries))
		for i, d := range deliveries {
			ids[i] = d.ID
		}
		r.db.WithContext(ctx).
			Model(&entities.Delivery{}).
			Where("id IN ?", ids).
			Update("status", entities.DeliveryStatusInProgress)
	}

	return deliveries, nil
}

func (r *deliveryRepository) Update(ctx context.Context, delivery *entities.Delivery) error {
	return r.db.WithContext(ctx).Save(delivery).Error
}
