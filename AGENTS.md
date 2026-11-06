# Project: Webhook Delivery Service

A backend service that lets customers register webhook URLs, accepts events through a REST API, and reliably delivers them to those URLs using background workers.

## Tech
- Go (latest stable), standard library net/http with Go 1.22+ routing patterns (for example mux.HandleFunc with 'POST /events/{id}')
- PostgreSQL via pgx
- RabbitMQ via amqp091-go
- Docker and docker compose for everything
- log/slog for structured logging

## Layout
- cmd/api: the HTTP API binary
- cmd/worker: the background delivery worker binary
- internal/config: environment config
- internal/db: database access and migrations runner
- internal/handlers: HTTP handlers
- internal/queue: RabbitMQ publish and consume
- internal/delivery: delivery logic, signing, retries
- migrations/: numbered .sql files

## Rules
- Only do what the current prompt asks. Do not build ahead.
- Keep code simple and readable. Add short comments explaining non-obvious decisions.
- Handle every error explicitly. Never ignore errors.
- Configuration comes from environment variables with sensible defaults for local dev.
- Every feature needs tests. Use table-driven tests where it makes sense.
- Do not add a library without telling me what it is and why.
- When you finish, tell me: 1) what files you created or changed, 2) the exact commands to run it, 3) the exact commands to test it, 4) what I should see if it works.
