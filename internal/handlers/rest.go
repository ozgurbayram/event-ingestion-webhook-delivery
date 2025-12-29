package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"event-intestion/internal/entities"
	"event-intestion/internal/ingestor"
	"event-intestion/internal/repositories"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type RESTHandler struct {
	ingestorService ingestor.Service
	webhookRepo     repositories.WebhookRepository
}

func NewRESTHandler(ingestorService ingestor.Service, webhookRepo repositories.WebhookRepository) *RESTHandler {
	return &RESTHandler{
		ingestorService: ingestorService,
		webhookRepo:     webhookRepo,
	}
}

func (h *RESTHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/events", h.IngestEvent).Methods(http.MethodPost)
	r.HandleFunc("/webhooks", h.CreateWebhook).Methods(http.MethodPost)
	r.HandleFunc("/webhooks/{id}", h.GetWebhook).Methods(http.MethodGet)
	r.HandleFunc("/webhooks/{id}", h.UpdateWebhook).Methods(http.MethodPut)
	r.HandleFunc("/webhooks/{id}", h.DeleteWebhook).Methods(http.MethodDelete)
	r.HandleFunc("/health", h.HealthCheck).Methods(http.MethodGet)
}

type IngestEventRequest struct {
	ApplicationID  string                 `json:"application_id"`
	EventType      string                 `json:"event_type"`
	IdempotencyKey string                 `json:"idempotency_key"`
	Payload        map[string]interface{} `json:"payload"`
	OccurredAt     *time.Time             `json:"occurred_at,omitempty"`
}

type IngestEventResponse struct {
	EventID         string `json:"event_id,omitempty"`
	DeliveriesCount int    `json:"deliveries_count"`
	Status          string `json:"status"`
}

func (h *RESTHandler) IngestEvent(w http.ResponseWriter, r *http.Request) {
	var req IngestEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.ApplicationID == "" || req.EventType == "" || req.IdempotencyKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "application_id, event_type, and idempotency_key are required"})
		return
	}

	occurredAt := time.Now()
	if req.OccurredAt != nil {
		occurredAt = *req.OccurredAt
	}

	resp, err := h.ingestorService.Ingest(r.Context(), ingestor.IngestRequest{
		ApplicationID:  req.ApplicationID,
		EventType:      req.EventType,
		IdempotencyKey: req.IdempotencyKey,
		Payload:        req.Payload,
		Source:         entities.EventSourceREST,
		OccurredAt:     occurredAt,
	})

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if resp.AlreadyProcessed {
		writeJSON(w, http.StatusOK, IngestEventResponse{
			Status: "already_processed",
		})
		return
	}

	writeJSON(w, http.StatusCreated, IngestEventResponse{
		EventID:         resp.EventID,
		DeliveriesCount: resp.DeliveriesCount,
		Status:          "created",
	})
}

type CreateWebhookRequest struct {
	ApplicationID string   `json:"application_id"`
	URL           string   `json:"url"`
	Secret        string   `json:"secret"`
	EventTypes    []string `json:"event_types"`
}

type WebhookResponse struct {
	ID            string   `json:"id"`
	ApplicationID string   `json:"application_id"`
	URL           string   `json:"url"`
	EventTypes    []string `json:"event_types"`
	Active        bool     `json:"active"`
	CreatedAt     string   `json:"created_at"`
}

func (h *RESTHandler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	var req CreateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.ApplicationID == "" || req.URL == "" || req.Secret == "" || len(req.EventTypes) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "application_id, url, secret, and event_types are required"})
		return
	}

	webhook := entities.NewWebhook(req.ApplicationID, req.URL, req.Secret, req.EventTypes)
	if err := h.webhookRepo.Create(r.Context(), webhook); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, WebhookResponse{
		ID:            webhook.ID.String(),
		ApplicationID: webhook.ApplicationID,
		URL:           webhook.URL,
		EventTypes:    webhook.EventTypes,
		Active:        webhook.Active,
		CreatedAt:     webhook.CreatedAt.Format(time.RFC3339),
	})
}

func (h *RESTHandler) GetWebhook(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := uuid.Parse(vars["id"])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid webhook id"})
		return
	}

	webhook, err := h.webhookRepo.GetByID(r.Context(), id)
	if err != nil {
		if err == repositories.ErrWebhookNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, WebhookResponse{
		ID:            webhook.ID.String(),
		ApplicationID: webhook.ApplicationID,
		URL:           webhook.URL,
		EventTypes:    webhook.EventTypes,
		Active:        webhook.Active,
		CreatedAt:     webhook.CreatedAt.Format(time.RFC3339),
	})
}

type UpdateWebhookRequest struct {
	URL        *string  `json:"url,omitempty"`
	Secret     *string  `json:"secret,omitempty"`
	EventTypes []string `json:"event_types,omitempty"`
	Active     *bool    `json:"active,omitempty"`
}

func (h *RESTHandler) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := uuid.Parse(vars["id"])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid webhook id"})
		return
	}

	webhook, err := h.webhookRepo.GetByID(r.Context(), id)
	if err != nil {
		if err == repositories.ErrWebhookNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var req UpdateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.URL != nil {
		webhook.URL = *req.URL
	}
	if req.Secret != nil {
		webhook.Secret = *req.Secret
	}
	if req.EventTypes != nil {
		webhook.EventTypes = req.EventTypes
	}
	if req.Active != nil {
		webhook.Active = *req.Active
	}

	if err := h.webhookRepo.Update(r.Context(), webhook); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, WebhookResponse{
		ID:            webhook.ID.String(),
		ApplicationID: webhook.ApplicationID,
		URL:           webhook.URL,
		EventTypes:    webhook.EventTypes,
		Active:        webhook.Active,
		CreatedAt:     webhook.CreatedAt.Format(time.RFC3339),
	})
}

func (h *RESTHandler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := uuid.Parse(vars["id"])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid webhook id"})
		return
	}

	if err := h.webhookRepo.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *RESTHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
