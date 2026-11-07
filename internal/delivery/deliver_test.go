package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/divs-lab/webhook-server/internal/db"
)

type mockStore struct {
	mu        sync.Mutex
	event     db.Event
	endpoint  db.Endpoint
	newStatus string
}

func (m *mockStore) GetEvent(ctx context.Context, id string) (db.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.event, nil
}

func (m *mockStore) GetEndpoint(ctx context.Context, id string) (db.Endpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.endpoint, nil
}

func (m *mockStore) UpdateStatus(ctx context.Context, id string, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.newStatus = status
	return nil
}

func TestDeliverEvent(t *testing.T) {
	const eventID = "evt-12345"
	const endpointID = "ep-12345"
	payload := json.RawMessage(`{"ping":"pong","num":42}`)

	t.Run("success 200 updates status to delivered", func(t *testing.T) {
		var receivedHeaderEventID string
		var receivedContentType string
		var receivedBody string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedHeaderEventID = r.Header.Get("X-Event-Id")
			receivedContentType = r.Header.Get("Content-Type")
			bodyBytes, _ := io.ReadAll(r.Body)
			receivedBody = string(bodyBytes)

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"received":true}`))
		}))
		defer server.Close()

		store := &mockStore{
			event: db.Event{
				ID:         eventID,
				EndpointID: endpointID,
				Payload:    payload,
			},
			endpoint: db.Endpoint{
				ID:  endpointID,
				URL: server.URL,
			},
		}

		svc := NewService(store, server.Client())
		if err := svc.DeliverEvent(context.Background(), eventID); err != nil {
			t.Fatalf("DeliverEvent: %v", err)
		}

		if store.newStatus != StatusDelivered {
			t.Errorf("status = %q, want %q", store.newStatus, StatusDelivered)
		}
		if receivedHeaderEventID != eventID {
			t.Errorf("X-Event-Id header = %q, want %q", receivedHeaderEventID, eventID)
		}
		if receivedContentType != "application/json" {
			t.Errorf("Content-Type header = %q, want application/json", receivedContentType)
		}
		if receivedBody != string(payload) {
			t.Errorf("body = %q, want %q", receivedBody, string(payload))
		}
	})

	t.Run("500 response updates status to failed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "server error", http.StatusInternalServerError)
		}))
		defer server.Close()

		store := &mockStore{
			event: db.Event{
				ID:         eventID,
				EndpointID: endpointID,
				Payload:    payload,
			},
			endpoint: db.Endpoint{
				ID:  endpointID,
				URL: server.URL,
			},
		}

		svc := NewService(store, server.Client())
		if err := svc.DeliverEvent(context.Background(), eventID); err != nil {
			t.Fatalf("DeliverEvent: %v", err)
		}

		if store.newStatus != StatusFailed {
			t.Errorf("status = %q, want %q", store.newStatus, StatusFailed)
		}
	})

	t.Run("timeout updates status to failed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Sleep longer than the client's timeout
			time.Sleep(150 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		store := &mockStore{
			event: db.Event{
				ID:         eventID,
				EndpointID: endpointID,
				Payload:    payload,
			},
			endpoint: db.Endpoint{
				ID:  endpointID,
				URL: server.URL,
			},
		}

		fastClient := &http.Client{Timeout: 30 * time.Millisecond}
		svc := NewService(store, fastClient)

		if err := svc.DeliverEvent(context.Background(), eventID); err != nil {
			t.Fatalf("DeliverEvent: %v", err)
		}

		if store.newStatus != StatusFailed {
			t.Errorf("status = %q, want %q", store.newStatus, StatusFailed)
		}
	})

	t.Run("unreachable server updates status to failed", func(t *testing.T) {
		store := &mockStore{
			event: db.Event{
				ID:         eventID,
				EndpointID: endpointID,
				Payload:    payload,
			},
			endpoint: db.Endpoint{
				ID:  endpointID,
				URL: "http://127.0.0.1:59999/unreachable", // Port closed
			},
		}

		svc := NewService(store, &http.Client{Timeout: 100 * time.Millisecond})
		if err := svc.DeliverEvent(context.Background(), eventID); err != nil {
			t.Fatalf("DeliverEvent: %v", err)
		}

		if store.newStatus != StatusFailed {
			t.Errorf("status = %q, want %q", store.newStatus, StatusFailed)
		}
	})
}
