package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/divs-lab/webhook-server/internal/config"
	"github.com/divs-lab/webhook-server/internal/db"
	"github.com/divs-lab/webhook-server/internal/delivery"
	"github.com/divs-lab/webhook-server/internal/queue"
)

type workerStore struct {
	events    *db.EventRepo
	endpoints *db.EndpointRepo
}

func (s *workerStore) GetEvent(ctx context.Context, id string) (db.Event, error) {
	return s.events.GetEvent(ctx, id)
}

func (s *workerStore) GetEndpoint(ctx context.Context, id string) (db.Endpoint, error) {
	return s.endpoints.Get(ctx, id)
}

func (s *workerStore) UpdateStatus(ctx context.Context, id string, status string) error {
	return s.events.UpdateStatus(ctx, id, status)
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStartup()

	pool, err := db.Open(startupCtx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer pool.Close()

	// Prefetch count of 10 gives the worker a batch of jobs without overwhelming memory
	consumer, err := queue.NewConsumer(cfg.RabbitMQURL, 10)
	if err != nil {
		return fmt.Errorf("connect rabbitmq consumer: %w", err)
	}
	defer consumer.Close()

	store := &workerStore{
		events:    db.NewEventRepo(pool),
		endpoints: db.NewEndpointRepo(pool),
	}

	deliverService := delivery.NewService(store, nil)

	// Listen for SIGINT / SIGTERM for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("worker starting, listening for delivery jobs", "queue", queue.DeliverQueue)

	consumeErrCh := make(chan error, 1)
	go func() {
		consumeErrCh <- consumer.Consume(ctx, func(jobCtx context.Context, eventID string) error {
			return deliverService.DeliverEvent(jobCtx, eventID)
		})
	}()

	select {
	case err := <-consumeErrCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("consumer error: %w", err)
		}
		return nil
	case <-ctx.Done():
		slog.Info("shutdown signal received, waiting for in-flight deliveries (max 30s)")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		select {
		case <-consumeErrCh:
			slog.Info("worker shutdown complete")
			return nil
		case <-shutdownCtx.Done():
			slog.Warn("shutdown timed out, forcing exit")
			return shutdownCtx.Err()
		}
	}
}
