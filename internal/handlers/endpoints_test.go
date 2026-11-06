package handlers

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/divs-lab/webhook-server/internal/db"
)

type mockEndpointStore struct {
	endpoints map[string]db.Endpoint
}

func newMockEndpointStore() *mockEndpointStore {
	return &mockEndpointStore{
		endpoints: make(map[string]db.Endpoint),
	}
}

func (m *mockEndpointStore) Create(ctx context.Context, targetURL, secret string) (db.Endpoint, error) {
	id := "550e8400-e29b-41d4-a716-446655440000"
	ep := db.Endpoint{
		ID:        id,
		URL:       targetURL,
		Secret:    secret,
		CreatedAt: time.Now().UTC(),
	}
	m.endpoints[id] = ep
	return ep, nil
}

func (m *mockEndpointStore) List(ctx context.Context) ([]db.Endpoint, error) {
	var list []db.Endpoint
	for _, ep := range m.endpoints {
		list = append(list, db.Endpoint{
			ID:        ep.ID,
			URL:       ep.URL,
			CreatedAt: ep.CreatedAt,
			// Secret is deliberately omitted in List
		})
	}
	if list == nil {
		list = []db.Endpoint{}
	}
	return list, nil
}

func (m *mockEndpointStore) Get(ctx context.Context, id string) (db.Endpoint, error) {
	ep, ok := m.endpoints[id]
	if !ok {
		return db.Endpoint{}, db.ErrNotFound
	}
	return db.Endpoint{
		ID:        ep.ID,
		URL:       ep.URL,
		CreatedAt: ep.CreatedAt,
		// Secret is deliberately omitted in Get
	}, nil
}

func TestCreateEndpoint(t *testing.T) {
	t.Run("successful creation generates 32-byte hex secret", func(t *testing.T) {
		store := newMockEndpointStore()
		handler := CreateEndpoint(store)

		body := `{"url":"https://example.com/webhook"}`
		req := httptest.NewRequest(http.MethodPost, "/endpoints", bytes.NewBufferString(body))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
		}

		var created db.Endpoint
		if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		if created.ID == "" {
			t.Error("expected non-empty endpoint id")
		}
		if created.URL != "https://example.com/webhook" {
			t.Errorf("url = %q, want https://example.com/webhook", created.URL)
		}
		if created.Secret == "" {
			t.Fatal("expected secret to be present on creation")
		}

		secretBytes, err := hex.DecodeString(created.Secret)
		if err != nil {
			t.Fatalf("secret is not valid hex: %v", err)
		}
		if len(secretBytes) != 32 {
			t.Errorf("secret length = %d bytes, want 32", len(secretBytes))
		}
	})

	urlTests := []struct {
		name       string
		body       string
		wantStatus int
		wantErr    string
	}{
		{
			name:       "empty url",
			body:       `{"url":""}`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "url is required",
		},
		{
			name:       "ftp scheme is rejected",
			body:       `{"url":"ftp://example.com/hook"}`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "url scheme must be http or https",
		},
		{
			name:       "malformed url missing host",
			body:       `{"url":"http://"}`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "url host is required",
		},
		{
			name:       "invalid json syntax",
			body:       `{not-json}`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "invalid request body",
		},
	}

	for _, tc := range urlTests {
		t.Run(tc.name, func(t *testing.T) {
			store := newMockEndpointStore()
			handler := CreateEndpoint(store)

			req := httptest.NewRequest(http.MethodPost, "/endpoints", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}

			var errResp map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if errResp["error"] != tc.wantErr {
				t.Errorf("error = %q, want %q", errResp["error"], tc.wantErr)
			}
		})
	}
}

func TestListEndpoints(t *testing.T) {
	store := newMockEndpointStore()
	_, _ = store.Create(context.Background(), "https://example.com/hook", "super-secret-key")

	handler := ListEndpoints(store)

	req := httptest.NewRequest(http.MethodGet, "/endpoints", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var endpoints []db.Endpoint
	if err := json.NewDecoder(rec.Body).Decode(&endpoints); err != nil {
		t.Fatalf("decode list: %v", err)
	}

	if len(endpoints) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(endpoints))
	}

	// Rule: Secret must not be leaked on list
	if endpoints[0].Secret != "" {
		t.Errorf("secret was leaked on list: %q", endpoints[0].Secret)
	}
}

func TestGetEndpoint(t *testing.T) {
	store := newMockEndpointStore()
	created, _ := store.Create(context.Background(), "https://example.com/hook", "super-secret-key")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /endpoints/{id}", GetEndpoint(store))

	t.Run("valid uuid fetches endpoint without secret", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/endpoints/"+created.ID, nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var ep db.Endpoint
		if err := json.NewDecoder(rec.Body).Decode(&ep); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if ep.ID != created.ID {
			t.Errorf("id = %q, want %q", ep.ID, created.ID)
		}
		if ep.Secret != "" {
			t.Errorf("secret was leaked on get: %q", ep.Secret)
		}
	})

	t.Run("invalid uuid returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/endpoints/not-a-valid-uuid", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("missing endpoint returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/endpoints/00000000-0000-0000-0000-000000000000", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}
