package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/divs-lab/webhook-server/internal/db"
)

type mockEventStore struct {
	mu        sync.Mutex
	endpoints map[string]bool
	events    map[string]db.Event
	// key: endpointID + ":" + idempotencyKey -> eventID
	idempotencyMap map[string]string
}

func newMockEventStore() *mockEventStore {
	return &mockEventStore{
		endpoints:      make(map[string]bool),
		events:         make(map[string]db.Event),
		idempotencyMap: make(map[string]string),
	}
}

func (m *mockEventStore) EndpointExists(ctx context.Context, endpointID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.endpoints[endpointID], nil
}

func (m *mockEventStore) InsertEvent(ctx context.Context, endpointID string, payload json.RawMessage, idempotencyKey string) (db.Event, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	mapKey := endpointID + ":" + idempotencyKey
	if existingID, exists := m.idempotencyMap[mapKey]; exists {
		return m.events[existingID], false, nil
	}

	ev := db.Event{
		ID:             "660e8400-e29b-41d4-a716-446655440001",
		EndpointID:     endpointID,
		Payload:        payload,
		IdempotencyKey: idempotencyKey,
		Status:         "pending",
		CreatedAt:      time.Now().UTC(),
	}
	m.events[ev.ID] = ev
	m.idempotencyMap[mapKey] = ev.ID
	return ev, true, nil
}

func (m *mockEventStore) GetEvent(ctx context.Context, id string) (db.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ev, ok := m.events[id]
	if !ok {
		return db.Event{}, db.ErrNotFound
	}
	return ev, nil
}

func TestCreateEvent(t *testing.T) {
	validEndpointID := "550e8400-e29b-41d4-a716-446655440000"

	t.Run("new event returns 201", func(t *testing.T) {
		store := newMockEventStore()
		store.endpoints[validEndpointID] = true
		handler := CreateEvent(store)

		body := `{"endpoint_id":"` + validEndpointID + `","payload":{"order_id":123,"total":99.9}}`
		req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
		req.Header.Set("Idempotency-Key", "test-key-001")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
		}

		var ev db.Event
		if err := json.NewDecoder(rec.Body).Decode(&ev); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if ev.ID != "660e8400-e29b-41d4-a716-446655440001" {
			t.Errorf("id = %q, want 660e8400-e29b-41d4-a716-446655440001", ev.ID)
		}
		if ev.Status != "pending" {
			t.Errorf("status = %q, want pending", ev.Status)
		}
	})

	t.Run("duplicate key returns 200 with the same id", func(t *testing.T) {
		store := newMockEventStore()
		store.endpoints[validEndpointID] = true
		handler := CreateEvent(store)

		body := `{"endpoint_id":"` + validEndpointID + `","payload":{"order_id":123}}`

		// Request 1: New insert -> 201
		req1 := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
		req1.Header.Set("Idempotency-Key", "duplicate-key-1")
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusCreated {
			t.Fatalf("req1 status = %d, want 201", rec1.Code)
		}
		var ev1 db.Event
		_ = json.NewDecoder(rec1.Body).Decode(&ev1)

		// Request 2: Duplicate key -> 200 OK with same ID
		req2 := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
		req2.Header.Set("Idempotency-Key", "duplicate-key-1")
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("req2 status = %d, want 200", rec2.Code)
		}
		var ev2 db.Event
		_ = json.NewDecoder(rec2.Body).Decode(&ev2)

		if ev1.ID != ev2.ID {
			t.Fatalf("expected identical event ids on duplicate key: %q != %q", ev1.ID, ev2.ID)
		}
	})

	t.Run("missing header returns 400", func(t *testing.T) {
		store := newMockEventStore()
		handler := CreateEvent(store)

		body := `{"endpoint_id":"` + validEndpointID + `","payload":{"test":true}}`
		req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
		// No Idempotency-Key header set
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("oversized body is rejected", func(t *testing.T) {
		store := newMockEventStore()
		store.endpoints[validEndpointID] = true
		handler := CreateEvent(store)

		// Create body larger than 1 MB
		hugePayload := strings.Repeat("A", 1024*1024+50)
		body := `{"endpoint_id":"` + validEndpointID + `","payload":{"data":"` + hugePayload + `"}}`

		req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
		req.Header.Set("Idempotency-Key", "huge-key")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusRequestEntityTooLarge && rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 413 or 400", rec.Code)
		}
	})

	t.Run("non-existent endpoint returns 404", func(t *testing.T) {
		store := newMockEventStore()
		// endpoint not registered in store
		handler := CreateEvent(store)

		body := `{"endpoint_id":"` + validEndpointID + `","payload":{"test":true}}`
		req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
		req.Header.Set("Idempotency-Key", "key-missing-ep")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}

func TestGetEvent(t *testing.T) {
	store := newMockEventStore()
	store.endpoints["550e8400-e29b-41d4-a716-446655440000"] = true
	ev, _, _ := store.InsertEvent(context.Background(), "550e8400-e29b-41d4-a716-446655440000", json.RawMessage(`{"ping":"pong"}`), "get-key")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /events/{id}", GetEvent(store))

	t.Run("fetch existing event returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/events/"+ev.ID, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		var got db.Event
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.ID != ev.ID {
			t.Errorf("id = %q, want %q", got.ID, ev.ID)
		}
	})

	t.Run("invalid uuid returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/events/not-a-uuid", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("missing event returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/events/00000000-0000-0000-0000-000000000000", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}
