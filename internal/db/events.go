package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Event represents an ingested webhook event to be delivered.
type Event struct {
	ID             string          `json:"id"`
	EndpointID     string          `json:"endpoint_id"`
	Payload        json.RawMessage `json:"payload"`
	IdempotencyKey string          `json:"idempotency_key"`
	Status         string          `json:"status"`
	CreatedAt      time.Time       `json:"created_at"`
}

// EventRepo handles database operations for webhook events.
type EventRepo struct {
	pool *pgxpool.Pool
}

// NewEventRepo constructs a repository for events.
func NewEventRepo(pool *pgxpool.Pool) *EventRepo {
	return &EventRepo{pool: pool}
}

// EndpointExists checks whether an endpoint exists in the database.
func (r *EventRepo) EndpointExists(ctx context.Context, endpointID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM endpoints WHERE id = $1)`, endpointID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check endpoint exists: %w", err)
	}
	return exists, nil
}

// InsertEvent atomically inserts a new event or retrieves the existing one if
// (endpoint_id, idempotency_key) already exists. Returns isNew=true on insert (201)
// or isNew=false on duplicate key (200).
func (r *EventRepo) InsertEvent(ctx context.Context, endpointID string, payload json.RawMessage, idempotencyKey string) (Event, bool, error) {
	query := `
		WITH inserted AS (
			INSERT INTO events (endpoint_id, payload, idempotency_key, status)
			VALUES ($1, $2, $3, 'pending')
			ON CONFLICT (endpoint_id, idempotency_key) DO NOTHING
			RETURNING id, endpoint_id, payload, idempotency_key, status, created_at, true AS is_new
		)
		SELECT id, endpoint_id, payload, idempotency_key, status, created_at, is_new FROM inserted
		UNION ALL
		SELECT id, endpoint_id, payload, idempotency_key, status, created_at, false AS is_new
		FROM events
		WHERE endpoint_id = $1 AND idempotency_key = $3
		  AND NOT EXISTS (SELECT 1 FROM inserted);
	`

	var ev Event
	var isNew bool
	var rawPayload []byte

	err := r.pool.QueryRow(ctx, query, endpointID, []byte(payload), idempotencyKey).Scan(
		&ev.ID,
		&ev.EndpointID,
		&rawPayload,
		&ev.IdempotencyKey,
		&ev.Status,
		&ev.CreatedAt,
		&isNew,
	)
	if err != nil {
		return Event{}, false, fmt.Errorf("insert event: %w", err)
	}
	ev.Payload = json.RawMessage(rawPayload)
	return ev, isNew, nil
}

// GetEvent fetches an event by its ID.
func (r *EventRepo) GetEvent(ctx context.Context, id string) (Event, error) {
	var ev Event
	var rawPayload []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, endpoint_id, payload, idempotency_key, status, created_at
		FROM events
		WHERE id = $1
	`, id).Scan(
		&ev.ID,
		&ev.EndpointID,
		&rawPayload,
		&ev.IdempotencyKey,
		&ev.Status,
		&ev.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Event{}, ErrNotFound
		}
		return Event{}, fmt.Errorf("get event: %w", err)
	}
	ev.Payload = json.RawMessage(rawPayload)
	return ev, nil
}

// UpdateStatus updates the delivery status of an event ('delivered' or 'failed').
func (r *EventRepo) UpdateStatus(ctx context.Context, id string, status string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE events
		SET status = $1
		WHERE id = $2
	`, status, id)
	if err != nil {
		return fmt.Errorf("update event status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
