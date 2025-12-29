package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"event-intestion/internal/entities"
)

type WebhookPayload struct {
	EventID       string                 `json:"event_id"`
	EventType     string                 `json:"event_type"`
	ApplicationID string                 `json:"application_id"`
	Payload       map[string]interface{} `json:"payload"`
	OccurredAt    string                 `json:"occurred_at"`
	DeliveredAt   string                 `json:"delivered_at"`
}

type Dispatcher struct {
	client  *http.Client
	signer  *Signer
	timeout time.Duration
}

func NewDispatcher(timeout time.Duration) *Dispatcher {
	return &Dispatcher{
		client: &http.Client{
			Timeout: timeout,
		},
		signer:  NewSigner(),
		timeout: timeout,
	}
}

type DispatchResult struct {
	Success    bool
	StatusCode int
	Error      string
}

func (d *Dispatcher) Dispatch(ctx context.Context, delivery *entities.Delivery) DispatchResult {
	if delivery.Event == nil || delivery.Webhook == nil {
		return DispatchResult{
			Success: false,
			Error:   "missing event or webhook data",
		}
	}

	payload := WebhookPayload{
		EventID:       delivery.Event.ID.String(),
		EventType:     delivery.Event.EventType,
		ApplicationID: delivery.Event.ApplicationID,
		Payload:       delivery.Event.Payload,
		OccurredAt:    delivery.Event.OccurredAt.Format(time.RFC3339),
		DeliveredAt:   time.Now().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return DispatchResult{
			Success: false,
			Error:   fmt.Sprintf("failed to marshal payload: %v", err),
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.Webhook.URL, bytes.NewReader(body))
	if err != nil {
		return DispatchResult{
			Success: false,
			Error:   fmt.Sprintf("failed to create request: %v", err),
		}
	}

	timestamp := time.Now()
	signature := d.signer.Sign(body, delivery.Webhook.Secret, timestamp)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signature)
	req.Header.Set("X-Event-ID", delivery.Event.ID.String())
	req.Header.Set("X-Event-Type", delivery.Event.EventType)

	resp, err := d.client.Do(req)
	if err != nil {
		return DispatchResult{
			Success: false,
			Error:   fmt.Sprintf("request failed: %v", err),
		}
	}
	defer resp.Body.Close()

	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return DispatchResult{
			Success:    true,
			StatusCode: resp.StatusCode,
		}
	}

	return DispatchResult{
		Success:    false,
		StatusCode: resp.StatusCode,
		Error:      fmt.Sprintf("received non-2xx status code: %d", resp.StatusCode),
	}
}
