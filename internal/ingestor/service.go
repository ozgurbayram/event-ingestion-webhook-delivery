package ingestor

import (
	"context"
	"errors"
	"time"

	"event-intestion/internal/entities"
	"event-intestion/internal/repositories"

	"gorm.io/gorm"
)

var ErrDuplicateEvent = errors.New("duplicate event: idempotency key already exists")
var ErrInvalidSource = errors.New("invalid event source")

type IngestRequest struct {
	ApplicationID  string
	EventType      string
	IdempotencyKey string
	Payload        map[string]interface{}
	Source         entities.EventSource
	OccurredAt     time.Time
}

type IngestResponse struct {
	EventID          string
	DeliveriesCount  int
	AlreadyProcessed bool
}

type Service interface {
	Ingest(ctx context.Context, req IngestRequest) (*IngestResponse, error)
}

type service struct {
	db           *gorm.DB
	eventRepo    repositories.EventRepository
	webhookRepo  repositories.WebhookRepository
	deliveryRepo repositories.DeliveryRepository
}

func NewService(
	db *gorm.DB,
	eventRepo repositories.EventRepository,
	webhookRepo repositories.WebhookRepository,
	deliveryRepo repositories.DeliveryRepository,
) Service {
	return &service{
		db:           db,
		eventRepo:    eventRepo,
		webhookRepo:  webhookRepo,
		deliveryRepo: deliveryRepo,
	}
}

func (s *service) Ingest(ctx context.Context, req IngestRequest) (*IngestResponse, error) {
	if !req.Source.IsValid() {
		return nil, ErrInvalidSource
	}

	exists, err := s.eventRepo.ExistsByIdempotencyKey(ctx, req.ApplicationID, req.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if exists {
		return &IngestResponse{AlreadyProcessed: true}, nil
	}

	event := entities.NewEvent(
		req.ApplicationID,
		req.EventType,
		req.IdempotencyKey,
		req.Payload,
		req.Source,
		req.OccurredAt,
	)

	var deliveriesCount int

	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.eventRepo.Create(ctx, tx, event); err != nil {
			if errors.Is(err, repositories.ErrDuplicateEvent) {
				return ErrDuplicateEvent
			}
			return err
		}

		webhooks, err := s.webhookRepo.GetActiveByApplicationID(ctx, req.ApplicationID)
		if err != nil {
			return err
		}

		var deliveries []*entities.Delivery
		for _, webhook := range webhooks {
			if webhook.SubscribesToEventType(req.EventType) {
				delivery := entities.NewDelivery(event.ID, webhook.ID)
				deliveries = append(deliveries, delivery)
			}
		}

		if len(deliveries) > 0 {
			if err := s.deliveryRepo.CreateBatch(ctx, tx, deliveries); err != nil {
				return err
			}
		}

		deliveriesCount = len(deliveries)
		return nil
	})

	if err != nil {
		if errors.Is(err, ErrDuplicateEvent) {
			return &IngestResponse{AlreadyProcessed: true}, nil
		}
		return nil, err
	}

	return &IngestResponse{
		EventID:         event.ID.String(),
		DeliveriesCount: deliveriesCount,
	}, nil
}
