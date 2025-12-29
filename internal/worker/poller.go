package worker

import (
	"context"
	"log"
	"math/rand"
	"sync"
	"time"

	"event-intestion/internal/entities"
	"event-intestion/internal/repositories"
)

type Poller struct {
	deliveryRepo    repositories.DeliveryRepository
	dispatcher      *Dispatcher
	pollingInterval time.Duration
	batchSize       int
	maxRetries      int
	wg              sync.WaitGroup
	stopCh          chan struct{}
}

func NewPoller(
	deliveryRepo repositories.DeliveryRepository,
	dispatcher *Dispatcher,
	pollingInterval time.Duration,
	batchSize int,
	maxRetries int,
) *Poller {
	return &Poller{
		deliveryRepo:    deliveryRepo,
		dispatcher:      dispatcher,
		pollingInterval: pollingInterval,
		batchSize:       batchSize,
		maxRetries:      maxRetries,
		stopCh:          make(chan struct{}),
	}
}

func (p *Poller) Start(ctx context.Context) {
	p.wg.Add(1)
	go p.run(ctx)
}

func (p *Poller) Stop() {
	close(p.stopCh)
	p.wg.Wait()
}

func (p *Poller) run(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.pollingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.processBatch(ctx)
		}
	}
}

func (p *Poller) processBatch(ctx context.Context) {
	deliveries, err := p.deliveryRepo.FetchPendingForProcessing(ctx, p.batchSize)
	if err != nil {
		log.Printf("failed to fetch pending deliveries: %v", err)
		return
	}

	for _, delivery := range deliveries {
		p.processDelivery(ctx, delivery)
	}
}

func (p *Poller) processDelivery(ctx context.Context, delivery *entities.Delivery) {
	result := p.dispatcher.Dispatch(ctx, delivery)

	if result.Success {
		delivery.MarkSuccess()
		log.Printf("delivery %s succeeded", delivery.ID)
	} else {
		nextRetry := p.calculateNextRetry(delivery.AttemptCount)
		delivery.MarkFailed(result.Error, nextRetry, p.maxRetries)

		if delivery.Status == entities.DeliveryStatusExhausted {
			log.Printf("delivery %s exhausted after %d attempts", delivery.ID, delivery.AttemptCount)
		} else {
			log.Printf("delivery %s failed, retry scheduled at %s", delivery.ID, nextRetry.Format(time.RFC3339))
		}
	}

	if err := p.deliveryRepo.Update(ctx, delivery); err != nil {
		log.Printf("failed to update delivery %s: %v", delivery.ID, err)
	}
}

func (p *Poller) calculateNextRetry(attemptCount int) time.Time {
	baseDelays := []time.Duration{
		1 * time.Minute,
		5 * time.Minute,
		15 * time.Minute,
		1 * time.Hour,
		4 * time.Hour,
	}

	idx := attemptCount
	if idx >= len(baseDelays) {
		idx = len(baseDelays) - 1
	}

	baseDelay := baseDelays[idx]
	jitter := time.Duration(float64(baseDelay) * 0.25 * (rand.Float64()*2 - 1))

	delay := baseDelay + jitter
	if delay < 0 {
		delay = baseDelay
	}

	return time.Now().Add(delay)
}
