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

---

## API Design

| Skill | Where used |
|---|---|
| RESTful resource design | `/volumes` CRUD with GET, POST, PUT, PATCH, DELETE |
| Standardized JSON envelope | `response.APIResponse` — `success`, `message`, `data`, `error` |
| Input validation without a framework | `validateVolumeCreate`, `validateVolumeUpdate`, `validateVolumePatch`, `validateID` |
| CORS middleware | `corsMiddleware` — origin whitelist, preflight OPTIONS handling |
| Rate limiting middleware | `rateLimitMiddleware` using `golang.org/x/time/rate.Limiter` per-IP |
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
| Swagger UI served from binary | `httpswagger.WrapHandler` at `/swagger/` |
