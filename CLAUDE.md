# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
make build              # Compile to ./main (native)
make build-linux        # Compile to ./main-linux (for EC2/Linux)

# Run
make run                # Run with development env
make run-staging        # Run with staging env
make run-prod           # Run with production env
make watch              # Live reload with air (requires air installed)

# Test
make test               # Run all unit tests
make itest              # Run integration tests (requires Docker for testcontainers)

# Database
make docker-run         # Start MongoDB via docker-compose
make docker-down        # Stop MongoDB

# Swagger docs regeneration
swag init -g cmd/api/main.go   # Regenerate docs/ folder
```

## Architecture

The API serves sermon/book data for the SBS (Sermon By Sermon) platform. It uses a layered architecture:

**Entry point:** `cmd/api/main.go` — initializes server and handles graceful shutdown (SIGINT/SIGTERM with 5s timeout).

**Server layer** (`internal/server/`):
- `server.go` — creates the HTTP server; reads `PORT` from env, initializes the MongoDB connection, sets timeouts (read: 10s, write: 30s, idle: 1m)
- `routes.go` — registers all routes on `http.ServeMux`, applies CORS and rate-limiting middleware

**Database layer** (`internal/database/`):
- `connection.go` — defines the `Service` interface and `Database` struct (MongoDB driver); connection string uses optional auth based on whether username/password env vars are set
- Separate files per entity: `volume.go`, `book.go`, `donation.go`
- Integration tests use **testcontainers-go** (requires Docker) to spin up ephemeral MongoDB instances

**Utilities:**
- `internal/response/` — standardized `APIResponse` JSON wrapper (`success`, `message`, `data`, `error`)
- `internal/cache/` — generic in-memory cache and TTL-based expiring cache

## Key API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check |
| GET | `/volumes` | List all volumes |
| POST | `/volumes` | Create volume |
| PUT | `/volumes/{id}` | Full volume update |
| PATCH | `/volumes/{id}` | Partial volume update |
| DELETE | `/volumes/{id}` | Delete volume |
| GET | `/app-volume-list/{volume_number}` | Get books/sermons by volume (supports `language` query param) |
| GET | `/donate` | Get PayPal donation URL |
| GET | `/swagger/` | Swagger UI |

## Environment Variables

Required in `.env` (or `.env.development`, `.env.staging`, `.env.production`):

```bash
PORT=8080
APP_ENV=development
BLUEPRINT_DB_HOST=localhost
BLUEPRINT_DB_PORT=27017
BLUEPRINT_DB_NAME=sbs_db
BLUEPRINT_DB_USERNAME=         # Optional — omit for unauthenticated connection
BLUEPRINT_DB_ROOT_PASSWORD=    # Optional
```

See `.env.example` for the full template.

## Middleware

- **CORS**: Hardcoded allowed origins (`http://localhost:8080`, EC2 IP, production domain). Returns 403 for non-whitelisted origins. Update origins in `internal/server/routes.go`.
- **Rate limiting**: 1 req/sec with burst of 2, keyed by client IP. Returns 429 with `Retry-After` header.

## Swagger Docs

Swagger annotations live in handler godoc comments in `routes.go`. After modifying annotations, regenerate with:
```
swag init -g internal/server/server.go --output docs
```
The general API info (`@title`, `@version`, `@BasePath`) lives in [internal/server/server.go](internal/server/server.go) package comment. The `docs/` folder is committed and served at `/swagger/`.
