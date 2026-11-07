package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/divs-lab/webhook-server/internal/db"
	"github.com/divs-lab/webhook-server/internal/jsonutil"
)

// EventStore abstracts the persistence operations needed by the event handlers.
type EventStore interface {
	EndpointExists(ctx context.Context, endpointID string) (bool, error)
	InsertEvent(ctx context.Context, endpointID string, payload json.RawMessage, idempotencyKey string) (db.Event, bool, error)
	GetEvent(ctx context.Context, id string) (db.Event, error)
}

type createEventRequest struct {
	EndpointID string          `json:"endpoint_id"`
	Payload    json.RawMessage `json:"payload"`
}

// CreateEvent handles POST /events with a required Idempotency-Key and 1 MB max body size.
// Returns 201 Created for a new event or 200 OK for an existing event with the same idempotency key.
func CreateEvent(store EventStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if idempotencyKey == "" {
			jsonutil.Error(w, http.StatusBadRequest, "missing Idempotency-Key header")
			return
		}

		// Enforce 1 MB request body limit to protect against memory exhaustion attacks.
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		var req createEventRequest
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&req); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				jsonutil.Error(w, http.StatusRequestEntityTooLarge, "request body exceeds 1 MB limit")
				return
			}
			jsonutil.Error(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if !uuidRegex.MatchString(req.EndpointID) {
			jsonutil.Error(w, http.StatusBadRequest, "invalid endpoint_id, must be a valid uuid")
			return
		}

		if len(req.Payload) == 0 || string(req.Payload) == "null" || !json.Valid(req.Payload) {
			jsonutil.Error(w, http.StatusBadRequest, "payload must be valid non-empty json")
			return
		}

		exists, err := store.EndpointExists(r.Context(), req.EndpointID)
		if err != nil {
			slog.Error("failed to verify endpoint existence", "endpoint_id", req.EndpointID, "error", err)
			jsonutil.Error(w, http.StatusInternalServerError, "failed to process event")
			return
		}
		if !exists {
			jsonutil.Error(w, http.StatusNotFound, "endpoint not found")
			return
		}

		ev, isNew, err := store.InsertEvent(r.Context(), req.EndpointID, req.Payload, idempotencyKey)
		if err != nil {
			slog.Error("failed to insert event", "error", err)
			jsonutil.Error(w, http.StatusInternalServerError, "failed to insert event")
			return
		}

		status := http.StatusOK
		if isNew {
			status = http.StatusCreated
		}

		if err := jsonutil.Write(w, status, ev); err != nil {
			slog.Error("failed to write response", "error", err)
		}
	}
}

// GetEvent handles GET /events/{id}.
// Returns 400 if the ID is not a valid UUID, 404 if not found, or 200 with the event record.
func GetEvent(store EventStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !uuidRegex.MatchString(id) {
			jsonutil.Error(w, http.StatusBadRequest, "invalid id, must be a valid uuid")
			return
		}

		ev, err := store.GetEvent(r.Context(), id)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				jsonutil.Error(w, http.StatusNotFound, "event not found")
				return
			}
			slog.Error("failed to get event", "id", id, "error", err)
			jsonutil.Error(w, http.StatusInternalServerError, "failed to get event")
			return
		}

		if err := jsonutil.Write(w, http.StatusOK, ev); err != nil {
			slog.Error("failed to write response", "error", err)
		}
	}
}
