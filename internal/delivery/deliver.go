package delivery

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/divs-lab/webhook-server/internal/db"
)

// Delivery status constants
const (
	StatusDelivered = "delivered"
	StatusFailed    = "failed"
)

// Store defines the database dependencies needed for webhook delivery.
type Store interface {
	GetEvent(ctx context.Context, id string) (db.Event, error)
	GetEndpoint(ctx context.Context, id string) (db.Endpoint, error)
	UpdateStatus(ctx context.Context, id string, status string) error
}

// HTTPClient represents any HTTP client that can execute a request.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Service coordinates loading event details, sending HTTP requests, and updating delivery status.
type Service struct {
	store  Store
	client HTTPClient
}

// NewService constructs a delivery service with the given store and HTTP client.
func NewService(store Store, client HTTPClient) *Service {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Service{
		store:  store,
		client: client,
	}
}

// DeliverEvent executes the delivery for the given event ID.
// It loads the event and its target endpoint, sends an HTTP POST request,
// and records the event status as 'delivered' (on 2xx) or 'failed' (on non-2xx or network errors).
func (s *Service) DeliverEvent(ctx context.Context, eventID string) error {
	ev, err := s.store.GetEvent(ctx, eventID)
	if err != nil {
		return fmt.Errorf("load event %s: %w", eventID, err)
	}

	ep, err := s.store.GetEndpoint(ctx, ev.EndpointID)
	if err != nil {
		return fmt.Errorf("load endpoint %s: %w", ev.EndpointID, err)
	}

	slog.Info("delivering webhook", "event_id", ev.ID, "endpoint_id", ep.ID, "url", ep.URL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(ev.Payload))
	if err != nil {
		_ = s.store.UpdateStatus(ctx, ev.ID, StatusFailed)
		return fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Event-Id", ev.ID)

	resp, err := s.client.Do(req)
	if err != nil {
		slog.Warn("delivery failed with network/timeout error", "event_id", ev.ID, "error", err)
		if updateErr := s.store.UpdateStatus(ctx, ev.ID, StatusFailed); updateErr != nil {
			return fmt.Errorf("update status to failed: %w", updateErr)
		}
		return nil
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	is2xx := resp.StatusCode >= 200 && resp.StatusCode <= 299
	newStatus := StatusFailed
	if is2xx {
		newStatus = StatusDelivered
		slog.Info("delivery succeeded", "event_id", ev.ID, "status_code", resp.StatusCode)
	} else {
		slog.Warn("delivery returned non-2xx status code", "event_id", ev.ID, "status_code", resp.StatusCode)
	}

	if err := s.store.UpdateStatus(ctx, ev.ID, newStatus); err != nil {
		return fmt.Errorf("update status to %s: %w", newStatus, err)
	}

	return nil
}
