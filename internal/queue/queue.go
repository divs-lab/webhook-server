package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	// DeliverQueue is the durable queue name for pending webhook deliveries.
	DeliverQueue = "events.deliver"
)

// Publisher publishes event delivery jobs to RabbitMQ.
type Publisher struct {
	conn    *amqp.Connection
	channel *amqp.Channel
}

// NewPublisher connects to RabbitMQ, opens a channel, and ensures the durable queue exists.
func NewPublisher(url string) (*Publisher, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}

	_, err = ch.QueueDeclare(
		DeliverQueue, // name
		true,         // durable
		false,        // auto-delete
		false,        // exclusive
		false,        // no-wait
		nil,          // args
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("declare queue %s: %w", DeliverQueue, err)
	}

	return &Publisher{conn: conn, channel: ch}, nil
}

// Publish enqueues an event ID as a persistent message.
func (p *Publisher) Publish(ctx context.Context, eventID string) error {
	msg := amqp.Publishing{
		DeliveryMode: amqp.Persistent,
		ContentType:  "text/plain",
		Body:         []byte(eventID),
		Timestamp:    time.Now().UTC(),
	}

	return p.channel.PublishWithContext(
		ctx,
		"",           // default exchange
		DeliverQueue, // routing key
		false,        // mandatory
		false,        // immediate
		msg,
	)
}

// Close closes the underlying channel and connection.
func (p *Publisher) Close() error {
	if err := p.channel.Close(); err != nil {
		slog.Error("close publisher channel", "error", err)
	}
	return p.conn.Close()
}

// Consumer consumes messages from events.deliver with manual acknowledgements.
type Consumer struct {
	conn    *amqp.Connection
	channel *amqp.Channel
}

// NewConsumer connects to RabbitMQ and configures the prefetch count.
func NewConsumer(url string, prefetchCount int) (*Consumer, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}

	_, err = ch.QueueDeclare(
		DeliverQueue, // name
		true,         // durable
		false,        // auto-delete
		false,        // exclusive
		false,        // no-wait
		nil,          // args
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("declare queue %s: %w", DeliverQueue, err)
	}

	if err := ch.Qos(prefetchCount, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("set qos: %w", err)
	}

	return &Consumer{conn: conn, channel: ch}, nil
}

// MessageHandler is invoked for each delivery message.
type MessageHandler func(ctx context.Context, eventID string) error

// Consume starts consuming messages until ctx is canceled.
func (c *Consumer) Consume(ctx context.Context, handler MessageHandler) error {
	deliveries, err := c.channel.Consume(
		DeliverQueue, // queue
		"",           // consumer tag
		false,        // auto-ack = false (manual ack)
		false,        // exclusive
		false,        // no-local
		false,        // no-wait
		nil,          // args
	)
	if err != nil {
		return fmt.Errorf("start consume: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d, ok := <-deliveries:
			if !ok {
				return nil
			}

			eventID := string(d.Body)
			slog.Info("processing delivery job", "event_id", eventID)

			if err := handler(ctx, eventID); err != nil {
				slog.Error("handler failed", "event_id", eventID, "error", err)
				if nackErr := d.Nack(false, true); nackErr != nil {
					slog.Error("failed to nack message", "event_id", eventID, "error", nackErr)
				}
				continue
			}

			// Ack only AFTER the database status is successfully updated
			if ackErr := d.Ack(false); ackErr != nil {
				slog.Error("failed to ack message", "event_id", eventID, "error", ackErr)
			}
		}
	}
}

// Close gracefully closes the channel and connection.
func (c *Consumer) Close() error {
	if err := c.channel.Close(); err != nil {
		slog.Error("close consumer channel", "error", err)
	}
	return c.conn.Close()
}
