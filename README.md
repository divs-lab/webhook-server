# Webhook Delivery Service

A backend service built with Go, PostgreSQL, and RabbitMQ that allows customers to register webhook URLs, accepts events through an authenticated REST API, and reliably delivers them using background workers.

## Tech Stack
- **Language:** Go (1.22+ routing)
- **Database:** PostgreSQL 16 (via `pgx/v5`)
- **Queue / Broker:** RabbitMQ 3 Management (via `amqp091-go`)
- **Containerization:** Docker & Docker Compose

## Quick Start

### 1. Start Infrastructure
Start PostgreSQL and RabbitMQ in Docker:
```bash
docker compose up -d
```
- PostgreSQL: `localhost:5433` (or `5432`)
- RabbitMQ AMQP: `localhost:5672`
- RabbitMQ Management Dashboard: [http://localhost:15672](http://localhost:15672) (User: `guest`, Pass: `guest`)

### 2. Start the API Server
In one terminal:
```bash
make run
# Or: go run ./cmd/api
```
The server listens on `http://localhost:8080`.

### 3. Start the Delivery Worker
In a second terminal:
```bash
make run-worker
# Or: go run ./cmd/worker
```
The worker listens on the `events.deliver` RabbitMQ queue with a prefetch count of 10.

## Running Tests
```bash
make test
# Or: go test ./...
```
