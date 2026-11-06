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

## Step 2: PostgreSQL, migrations, and database health check

A connection pool (`pgxpool.Pool`) maintains reusable, pre-warmed TCP connections to PostgreSQL. Establishing new TCP/TLS handshakes and authenticating on every HTTP request adds 20–50ms latency and exhausts database process limits. A pool borrows an idle connection for milliseconds during a query and returns it immediately.

**Flow through the code:**
1. `cmd/api/main.go` calls `config.Load()` to read `.env` and environment variables (`PORT`, `DATABASE_URL`).
2. `db.Open(ctx, cfg.DatabaseURL)` validates the URL, creates `pgxpool.Pool`, and pings the database with a 5s timeout.
3. `db.Migrate(ctx, pool, dir)` obtains a connection and acquires a PostgreSQL advisory lock (`pg_advisory_lock(724136)`). It creates `schema_migrations`, scans `migrations/*.sql` in alphanumeric order, and executes each unapplied file inside an explicit transaction (`tx.Begin`/`tx.Commit`), inserting the version string into `schema_migrations`.
4. `cmd/api/main.go` registers `handlers.Health(pool)` on `GET /health`.
5. When `GET /health` is hit, `handlers.Health` runs `db.Ping(ctx)` with a 2s timeout via the minimal `Pinger` interface. It returns 200 `{"status":"ok"}` if the ping succeeds, or 503 `{"status":"unavailable"}` if the ping fails.

**Design decisions:**
- A custom, lightweight migration runner avoids third-party ORM/migration bloat while keeping migrations transparent.
- PostgreSQL advisory locks guarantee distributed safety: when multiple API containers boot concurrently, only one runs migrations while others wait.
- Interface segregation: `handlers.Health` accepts `Pinger` rather than `*pgxpool.Pool`, decoupling the HTTP layer from the database driver and allowing fast unit testing with stubs.

**What could go wrong in production:**
- Unindexed DDL migrations on high-traffic tables can take exclusive table locks, causing queries to pile up and exhausting the connection pool.
- Health check cascading failure: if a struggling database is hammered by frequent health checks, the extra ping queries can worsen saturation.

**Interview questions:**
1. *Why use an advisory lock during migrations?* In a multi-replica deployment (Kubernetes/ECS), multiple containers start simultaneously. Without an advisory lock, replicas race to run migrations, causing duplicate execution or catalog deadlocks.
2. *Why pass a `Pinger` interface to the health handler instead of the concrete pool?* Interface segregation. Handlers only require a `Ping(ctx)` method. This allows unit testing with a mock/stub (`stubDB`) without needing a real database.
3. *What is the difference between a liveness check and a readiness check?* A liveness check detects hung processes to trigger restarts. A readiness check (like our database ping) tells the load balancer whether the instance can currently serve customer requests; returning 503 drops it from traffic rotation without forcibly killing the container.
