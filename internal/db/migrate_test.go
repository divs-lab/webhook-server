package db

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestMigrationsApply(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set; skipping migration integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	dir, err := MigrationsDir()
	if err != nil {
		t.Fatalf("migrations dir: %v", err)
	}

	if err := Migrate(ctx, pool, dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// A second run must not fail or apply the file again.
	if err := Migrate(ctx, pool, dir); err != nil {
		t.Fatalf("migrate again: %v", err)
	}

	var endpoints, events *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.endpoints')::text, to_regclass('public.events')::text`).Scan(&endpoints, &events); err != nil {
		t.Fatalf("lookup tables: %v", err)
	}
	if endpoints == nil || *endpoints == "" {
		t.Fatal("endpoints table was not created")
	}
	if events == nil || *events == "" {
		t.Fatal("events table was not created")
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations WHERE version = '0001_init.sql'`).Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("schema_migrations rows = %d, want 1", count)
	}
}
