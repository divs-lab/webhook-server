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

## Step 3: API key authentication and endpoint registration

We compare API keys in constant time (`crypto/subtle.ConstantTimeCompare`) because normal string comparison (`==`) short-circuits at the first differing byte. An attacker measuring millisecond/microsecond response variations over repeated calls can determine correct characters sequentially (timing attack). Constant-time evaluation always checks every byte. The webhook signing secret is shown only once on creation so read-only dashboard users or compromised list queries cannot steal secrets and forge webhook payloads.

**Flow through the code:**
1. Incoming requests enter `handlers.RequireAuth(apiKey)` in `internal/handlers/auth.go`. If the path is `/health`, it skips auth immediately. Otherwise, it inspects the `Authorization: Bearer <key>` header, compares via `subtle.ConstantTimeCompare`, and returns a 401 JSON error via `jsonutil.Error` if missing or wrong.
2. For `POST /endpoints`, `handlers.CreateEndpoint` in `internal/handlers/endpoints.go` decodes the JSON body and validates that the URL has an `http` or `https` scheme with a valid host.
3. `handlers.GenerateSecret()` uses `crypto/rand` to generate 32 cryptographically secure random bytes, hex-encoded (64 characters).
4. The handler calls `store.Create(...)` (implemented by `db.EndpointRepo` in `internal/db/endpoints.go`), inserting into Postgres and returning the endpoint with `id`, `url`, `secret`, and `created_at`.
5. For `GET /endpoints` and `GET /endpoints/{id}`, the repository explicitly omits the `secret` column from SQL queries, and the handler returns 200 with the secret field omitted (`omitempty`). `GET /endpoints/{id}` validates the UUID regex first, returning 400 if malformed.

**Design decisions:**
- Repository pattern: Database SQL queries are isolated in `internal/db/endpoints.go`. Handlers consume a small `EndpointStore` interface, decoupling HTTP routing from pgx and enabling mock-based unit tests.
- Reusable `jsonutil` package: Ensures consistent JSON serialization and error formatting `{"error":"..."}` across all endpoints.

**What could go wrong in production:**
- Memory exhaustion if `GET /endpoints` returns tens of thousands of records without pagination (addressed in later steps).
- SSRF (Server-Side Request Forgery) if customer endpoints target private network addresses (e.g., `http://169.254.169.254` or `http://localhost`). Private IP filtering should be added before delivery workers send requests.

**Interview questions:**
1. *What is a timing attack on string comparison, and how does Go prevent it?* Standard equality exits early on the first mismatched byte, creating measurable latency differences. `subtle.ConstantTimeCompare` performs a bitwise XOR over all bytes regardless of mismatches, returning `1` only if identical, leaking zero timing clues.
2. *Why define an `EndpointStore` interface in the handlers package rather than depending directly on `*db.EndpointRepo`?* In Go, consumers define interfaces. This enables complete test isolation—handlers can be unit-tested using an in-memory mock store without spinning up Postgres or executing real queries.
3. *Why should webhook signing secrets never be returned in GET requests?* Secrets are symmetric keys used to sign outgoing webhook payloads. Exposing them in read operations violates the principle of least privilege; any read-only credential or log leak exposes customers to payload forgery.
