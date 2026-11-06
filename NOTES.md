# Interview notes

## Step 1: HTTP server, health check, graceful shutdown

A request to `GET /health` hits `cmd/api/main.go`, which builds a `net/http` server and registers `handlers.Health` on Go 1.22's method-and-path pattern `GET /health`. `handlers.Health` sets `Content-Type: application/json` and writes `{"status":"ok"}`. `internal/config.Load` reads `PORT` (default `8080`), plus `DATABASE_URL`, `RABBITMQ_URL`, and `API_KEY` for later steps. Logging uses `log/slog` with a JSON handler so every line is one object.

On Ctrl+C (`SIGINT`) or `SIGTERM`, `signal.NotifyContext` cancels. `main` stops calling `ListenAndServe`'s accept loop via `http.Server.Shutdown` and waits up to 10 seconds for in-flight handlers. Shutdown is separate from startup so a bind failure still exits immediately.

The standard library server is enough here. A framework would add routing and middleware we do not need yet. `Shutdown` beats `os.Exit` because exit drops open responses. A 10 second limit keeps a stuck handler from hanging deploys.

In production a health check that only proves the process is listening will stay green while the database is down. Shutdown on Windows is mostly Ctrl+C; orchestrators send `SIGTERM`, which this process also handles. `ReadHeaderTimeout` limits slow header sends; it does not limit the whole request body.

Interview questions:

1. Why `Shutdown` instead of closing the listener? `Shutdown` stops accept, waits for active handlers, then closes idle connections. Closing the listener alone aborts requests already being served.
2. What does the 10 second timeout protect? A deploy or restart must finish. If a handler is stuck, `Shutdown` returns an error and the process exits instead of blocking the new version.
3. Why JSON logs? Log aggregators parse one JSON object per line. `slog` is in the standard library, so there is no extra logging dependency.
