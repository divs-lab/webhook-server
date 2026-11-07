package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when an expected database record does not exist.
var ErrNotFound = errors.New("not found")

// Endpoint represents a registered customer webhook destination.
type Endpoint struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Secret    string    `json:"secret,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// EndpointRepo provides data access for webhook endpoints.
type EndpointRepo struct {
	pool *pgxpool.Pool
}

// NewEndpointRepo constructs a new repository backed by the given connection pool.
func NewEndpointRepo(pool *pgxpool.Pool) *EndpointRepo {
	return &EndpointRepo{pool: pool}
}

// Create inserts a new webhook endpoint with its signing secret and returns the created record.
func (r *EndpointRepo) Create(ctx context.Context, targetURL, secret string) (Endpoint, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO endpoints (url, secret)
		VALUES ($1, $2)
		RETURNING id, url, secret, created_at
	`, targetURL, secret)

	var ep Endpoint
	if err := row.Scan(&ep.ID, &ep.URL, &ep.Secret, &ep.CreatedAt); err != nil {
		return Endpoint{}, fmt.Errorf("insert endpoint: %w", err)
	}
	return ep, nil
}

// List returns all registered endpoints ordered by newest first.
// The signing secret is deliberately excluded from the query so it is never exposed in bulk listings.
func (r *EndpointRepo) List(ctx context.Context) ([]Endpoint, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, url, created_at
		FROM endpoints
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list endpoints: %w", err)
	}
	defer rows.Close()

	var endpoints []Endpoint
	for rows.Next() {
		var ep Endpoint
		if err := rows.Scan(&ep.ID, &ep.URL, &ep.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan endpoint: %w", err)
		}
		endpoints = append(endpoints, ep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate endpoints: %w", err)
	}
	if endpoints == nil {
		endpoints = []Endpoint{}
	}
	return endpoints, nil
}

// Get fetches a single endpoint by its ID.
// The signing secret is deliberately excluded.
func (r *EndpointRepo) Get(ctx context.Context, id string) (Endpoint, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, url, created_at
		FROM endpoints
		WHERE id = $1
	`, id)

	var ep Endpoint
	if err := row.Scan(&ep.ID, &ep.URL, &ep.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Endpoint{}, ErrNotFound
		}
		return Endpoint{}, fmt.Errorf("get endpoint: %w", err)
	}
	return ep, nil
}

// GetEndpoint is an alias for Get.
func (r *EndpointRepo) GetEndpoint(ctx context.Context, id string) (Endpoint, error) {
	return r.Get(ctx, id)
}
