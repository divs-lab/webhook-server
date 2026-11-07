package db

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"
)

func TestEventRepo_IdempotentConcurrency(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set; skipping event concurrency test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	endpointRepo := NewEndpointRepo(pool)
	ep, err := endpointRepo.Create(ctx, "https://example.com/concurrency-test", "secret-test-key")
	if err != nil {
		t.Fatalf("create test endpoint: %v", err)
	}

	eventRepo := NewEventRepo(pool)
	idempotencyKey := "concurrent-key-" + ep.ID
	payload := json.RawMessage(`{"user_id":42,"action":"checkout"}`)

	const totalRequests = 20
	var wg sync.WaitGroup
	wg.Add(totalRequests)

	type result struct {
		ev    Event
		isNew bool
		err   error
	}
	results := make([]result, totalRequests)

	// Send 20 simultaneous requests with the exact same (endpoint_id, idempotency_key)
	startSignal := make(chan struct{})
	for i := 0; i < totalRequests; i++ {
		idx := i
		go func() {
			defer wg.Done()
			<-startSignal // sync start so all goroutines fire concurrently
			ev, isNew, err := eventRepo.InsertEvent(ctx, ep.ID, payload, idempotencyKey)
			results[idx] = result{ev: ev, isNew: isNew, err: err}
		}()
	}

	close(startSignal) // release all goroutines simultaneously
	wg.Wait()

	// Verify all succeeded and share the exact same event ID
	var createdCount int
	var sharedID string

	for i, res := range results {
		if res.err != nil {
			t.Fatalf("request %d failed: %v", i, res.err)
		}
		if sharedID == "" {
			sharedID = res.ev.ID
		} else if res.ev.ID != sharedID {
			t.Fatalf("mismatched event ID: got %s, expected %s", res.ev.ID, sharedID)
		}
		if res.isNew {
			createdCount++
		}
	}

	// Exactly one request must report isNew == true
	if createdCount != 1 {
		t.Fatalf("expected exactly 1 insert (isNew=true), got %d", createdCount)
	}

	// Query PostgreSQL directly: exactly 1 row must exist in the events table
	var rowCount int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE endpoint_id = $1 AND idempotency_key = $2`, ep.ID, idempotencyKey).Scan(&rowCount)
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("database has %d rows, expected exactly 1", rowCount)
	}
}
