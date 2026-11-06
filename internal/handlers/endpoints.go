package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"

	"github.com/divs-lab/webhook-server/internal/db"
	"github.com/divs-lab/webhook-server/internal/jsonutil"
)

var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// EndpointStore defines the database operations required by the endpoint handlers.
type EndpointStore interface {
	Create(ctx context.Context, targetURL, secret string) (db.Endpoint, error)
	List(ctx context.Context) ([]db.Endpoint, error)
	Get(ctx context.Context, id string) (db.Endpoint, error)
}

type createEndpointRequest struct {
	URL string `json:"url"`
}

// GenerateSecret produces a cryptographically secure 32-byte random hex string (64 characters).
func GenerateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func validateTargetURL(raw string) error {
	if raw == "" {
		return errors.New("url is required")
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return errors.New("invalid url format")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("url scheme must be http or https")
	}
	if u.Host == "" {
		return errors.New("url host is required")
	}
	return nil
}

// CreateEndpoint handles POST /endpoints.
// Generates a random 32-byte signing secret and returns 201 with the newly created endpoint and secret.
func CreateEndpoint(store EndpointStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createEndpointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonutil.Error(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if err := validateTargetURL(req.URL); err != nil {
			jsonutil.Error(w, http.StatusBadRequest, err.Error())
			return
		}

		secret, err := GenerateSecret()
		if err != nil {
			slog.Error("failed to generate signing secret", "error", err)
			jsonutil.Error(w, http.StatusInternalServerError, "failed to generate signing secret")
			return
		}

		ep, err := store.Create(r.Context(), req.URL, secret)
		if err != nil {
			slog.Error("failed to create endpoint", "error", err)
			jsonutil.Error(w, http.StatusInternalServerError, "failed to create endpoint")
			return
		}

		if err := jsonutil.Write(w, http.StatusCreated, ep); err != nil {
			slog.Error("failed to write response", "error", err)
		}
	}
}

// ListEndpoints handles GET /endpoints.
// Returns all registered endpoints without their signing secrets.
func ListEndpoints(store EndpointStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		endpoints, err := store.List(r.Context())
		if err != nil {
			slog.Error("failed to list endpoints", "error", err)
			jsonutil.Error(w, http.StatusInternalServerError, "failed to list endpoints")
			return
		}

		if err := jsonutil.Write(w, http.StatusOK, endpoints); err != nil {
			slog.Error("failed to write response", "error", err)
		}
	}
}

// GetEndpoint handles GET /endpoints/{id}.
// Returns 400 if the ID is not a valid UUID, 404 if not found, or 200 with the endpoint (excluding secret).
func GetEndpoint(store EndpointStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !uuidRegex.MatchString(id) {
			jsonutil.Error(w, http.StatusBadRequest, "invalid id, must be a valid uuid")
			return
		}

		ep, err := store.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				jsonutil.Error(w, http.StatusNotFound, "endpoint not found")
				return
			}
			slog.Error("failed to get endpoint", "id", id, "error", err)
			jsonutil.Error(w, http.StatusInternalServerError, "failed to get endpoint")
			return
		}

		if err := jsonutil.Write(w, http.StatusOK, ep); err != nil {
			slog.Error("failed to write response", "error", err)
		}
	}
}
