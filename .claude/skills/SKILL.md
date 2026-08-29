# skills.md

Skills, patterns, and concepts practiced in this project.

---

## Go Language

| Skill | Where used |
|---|---|
| Interfaces for dependency injection | `database.Service` interface consumed by `server.Server` |
| Anonymous struct types as function parameters | `validateVolumeCreate`, `validateVolumeUpdate` in `routes.go` |
| Generics | `internal/cache/cache.go` — `Cache[K, V]` |
| Goroutines + graceful shutdown | `cmd/api/main.go` — `SIGINT`/`SIGTERM` with 5 s context timeout |
| `net/http` standard library routing | `http.ServeMux` with Go 1.22 method+path patterns (`GET /volumes/{id}`) |
| `r.PathValue()` / `r.SetPathValue()` | Path parameter extraction (Go 1.22+) |
| `context.WithTimeout` | All MongoDB operations in `internal/database/` |
| Package-level vars for configuration | `connection.go` — env vars loaded at init time |
| `log/slog` JSON structured logging | `cmd/api/main.go` — slog set as default in `init()` |
| `crypto/rand` for request IDs | `internal/middleware/request_id.go` — 16-byte hex ID per request |
| `runtime/debug.Stack` in panic recovery | `internal/middleware/recover.go` — captures stack on panic |
| `sync.Pool` for hot-path allocations | `internal/middleware/gzip.go` — pooled `*gzip.Writer` |
| `time.Tick` background GC goroutine | `internal/middleware/rate_limit.go` — evicts idle per-IP limiters |

---

## API Design

| Skill | Where used |
|---|---|
| RESTful resource design | `/volumes` CRUD with GET, POST, PUT, PATCH, DELETE |
| Standardized JSON envelope | `response.APIResponse` — `success`, `message`, `data`, `error` |
| Input validation without a framework | `validateVolumeCreate`, `validateVolumeUpdate`, `validateVolumePatch`, `validateID` |
| CORS middleware | `corsMiddleware` — origin whitelist, preflight OPTIONS handling |
| Per-IP token-bucket rate limiting | `internal/middleware/rate_limit.go` — `*rate.Limiter` per `(ip, route)` keyed map with TTL eviction |
| Per-route rate-limit overrides | `buildMiddlewareChain` in `routes.go` — `/sermons/search` gets a stricter bucket than the default |
| Bypass list for health probes | `RateLimitConfig.Bypass` — `/health`, `/swagger/`, `/robots.txt` |
| Trusted-proxy `X-Forwarded-For` | `internal/middleware/client_ip.go` — only honored for source IPs in `TRUSTED_PROXY_CIDRS` |
| Panic recovery middleware | `internal/middleware/recover.go` — converts panic to 500, logs stack |
| Body-size limit | `internal/middleware/body_limit.go` — `http.MaxBytesReader` for POST/PUT/PATCH |
| Permissive CORS for GETs, allowlist for writes | `internal/middleware/cors.go` — env-driven `CORS_ALLOWED_ORIGINS` for write methods |
| `ReadHeaderTimeout` + `MaxHeaderBytes` | `internal/server/server.go` — Slowloris and large-header protection |
| Weak `ETag` + `Cache-Control` | `internal/middleware/cache_headers.go` — buffered SHA-256 hash, 304 on `If-None-Match` |
| CDN-friendly `s-maxage` | `DefaultCacheConfig` in `cache_headers.go` — long edge TTL with shorter browser TTL |
| Gzip compression with size threshold | `internal/middleware/gzip.go` — skips below 1 KiB |
| Security response headers | `internal/middleware/security_headers.go` — `X-Content-Type-Options`, `X-Frame-Options`, hidden `Server` |
| Structured access log | `internal/middleware/logging.go` — one `slog` line per request with status, duration, bytes, request_id |
| Static `/robots.txt` | `routes.go` `robotsHandler` — `Disallow: /` for unauthenticated public API |
| Env-gated Swagger UI | `routes.go` — `/swagger/` only mounted when `ENABLE_SWAGGER=true` |
| Swagger / OpenAPI annotations | `//	@Summary`, `//	@Router` godoc comments + `swaggo/swag` codegen |

---

## Database (MongoDB)

| Skill | Where used |
|---|---|
| MongoDB Go driver (`mongo-driver`) | All `internal/database/` operations |
| BSON document construction | `bson.M`, `bson.M{"$set": updates}` for queries and updates |
| Cursor iteration | `cursor.All(ctx, &results)` in `GetVolumes`, `GetBooksByVolume` |
| Upsert / replace patterns | `ReplaceOne` (UpdateVolume), `UpdateOne` with `$set` (PatchVolume) |
| Conditional connection string | Auth vs. no-auth MongoDB URI in `connection.go` |
| `mongodb+srv://` for Atlas | `buildURI()` in `connection.go` — switched on `MONGO_USE_SRV=true` |
| Connection pool tuning | `connection.go` — `SetMaxPoolSize`, `SetMinPoolSize`, server-selection / connect / socket timeouts |
| Retryable reads & writes | `connection.go` — `SetRetryWrites(true)`, `SetRetryReads(true)` |
| Programmatic index creation | `internal/database/indexes.go` — compound + text indexes ensured on startup |
| Text-search index (`$text`) | `books_text` in `indexes.go`; `SearchSermons` queries `$text` with weighted title/content |
| Aggregation pipeline `$sample` | `GetRandomSermon` — single random doc per language |
| Result-size caps on lists | `volumesListLimit`, `booksListLimit`, `searchResultLimit` constants |
| `Ping` in health check | `HealthCheck()` in `connection.go` — verifies live connection, not just process state |

---

## Testing

| Skill | Where used |
|---|---|
| `net/http/httptest` for handler unit tests | `routes_test.go` — `httptest.NewRecorder`, `httptest.NewServer` |
| Mock implementation of an interface | `mockService` struct in `routes_test.go` |
| Table-driven tests | `TestValidateVolumeCreate`, `TestValidateVolumePatch`, `TestValidateID`, `TestCreateVolume_ValidationErrors` |
| `r.SetPathValue()` in tests | Setting path params without a real mux in handler tests |
| `testcontainers-go` for integration tests | `connection_test.go` — ephemeral MongoDB container |
| Build tags to separate unit vs. integration | `//go:build integration` on database test files |
| `TestMain` setup/teardown | `connection_test.go` — container lifecycle around test suite |

---

## Tooling & Infrastructure

| Skill | Where used |
|---|---|
| `godotenv` auto-loading | `_ "github.com/joho/godotenv/autoload"` in server and database packages |
| `air` hot reload | `.air.toml` — rebuild on `.go` file changes |
| Multi-target Makefile | Build, run, test, itest, docker, watch targets |
| Docker Compose for local DB | `docker-compose.yml` — MongoDB with named volume |
| Systemd service unit | `sbs-engine.service` — production daemon management |
| Cross-compilation | `GOOS=linux GOARCH=amd64 go build` for EC2 deployment |
| Stripped + reproducible binary | `Makefile` — `-ldflags="-s -w" -trimpath` |
| Swagger UI served from binary | `httpswagger.WrapHandler` at `/swagger/`, env-gated |
| Generic in-memory `TTLCache` reused for two purposes | `internal/cache/expiring-cache.go` — used both for per-IP rate-limit eviction AND read-handler response caching |
| AWS deployment guide | `DEPLOYMENT.md` — CloudFront, WAF, ALB, alarms, costs |

---

## Prompt Templates

### Add a new API resource (full)

```
I want to add a new API resource called [RESOURCE_NAME] to this project.

Data shape:
- field_one: type (e.g. string, int, bool)
- field_two: type
- ...

Endpoints needed:
- GET    /[resource] — list all
- GET    /[resource]/{id} — get one
- POST   /[resource] — create
- PATCH  /[resource]/{id} — partial update
- DELETE /[resource]/{id} — delete

Business rules / validation:
- [field] must be greater than 0
- [field] is required, cannot be empty
- ...

Notes:
- [any special behavior, e.g. "filter by lang query param like sermons"]
- [any relationship to existing resources, e.g. "belongs to a volume"]

After implementing:
1. Add unit tests for all handlers and validators
2. Fix any broken tests
3. Regenerate Swagger docs with: swag init -g internal/server/server.go --output docs
4. Run make test to confirm everything passes
5. Return a report
```

### Add a new API resource (quick)

```
Add a [RESOURCE_NAME] resource with fields [field:type, ...].
CRUD endpoints: GET list, GET by id, POST, PATCH, DELETE.
Validation: [rules].
Add unit tests, regenerate Swagger, run make test, report back.
```
