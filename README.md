# sbs-engine

A public REST API that serves the **SBS (Spiritual Building Stones / Sermon By Sermon)** content library — volumes, books, sermons, and supporting metadata — to web and mobile clients.

The API is unauthenticated by design: anyone can read the content. Writes (create / update / delete) exist for administrative use and are protected at the network and CORS layers rather than by login.

## Stack

- **Language:** Go 1.25 (`net/http` standard-library router using Go 1.22+ method+path patterns)
- **Database:** MongoDB (local Docker container in dev, MongoDB Atlas in production)
- **Docs:** Swagger / OpenAPI generated from godoc annotations via [`swaggo/swag`](https://github.com/swaggo/swag), served at `/swagger/`
- **Process:** systemd unit on AWS EC2 (`sbs-engine.service`)
- **Local hot-reload:** [`air`](https://github.com/air-verse/air)

## Project layout

```
sbs-engine/
├── cmd/api/main.go                 # entry point, graceful shutdown
├── internal/
│   ├── server/                     # HTTP server, routing, handlers, middleware
│   │   ├── server.go               # http.Server config (timeouts, port)
│   │   ├── routes.go               # all route handlers + CORS / rate-limit middleware
│   │   └── routes_test.go          # handler unit tests with mock service
│   ├── database/                   # MongoDB layer
│   │   ├── connection.go           # client setup, Service interface, HealthCheck
│   │   ├── volume.go               # volumes CRUD
│   │   ├── book.go, sermon_ops.go  # sermons / books CRUD + search
│   │   ├── stats.go, donation.go   # ancillary endpoints
│   │   ├── language.go             # supported language registry
│   │   └── *_test.go               # unit + integration tests (testcontainers)
│   ├── cache/                      # generic + TTL caches (utility package)
│   ├── middleware/                 # cross-cutting concerns (CORS, rate limit, gzip, ETag, logging…)
│   └── response/                   # standard JSON envelope (success / error / data)
├── docs/                           # generated Swagger artefacts (swagger.json/yaml/docs.go)
├── docker-compose.yml              # local MongoDB
├── Makefile                        # build / run / test / docker / watch targets
├── .env.example                    # all configurable env vars (copy to .env.development etc.)
├── load-env.sh                     # loads .env.<env> into current shell
├── deploy.sh                       # produces a deployable bundle in ./deploy/
├── ec2-setup.sh                    # one-shot install on a fresh EC2 host
└── sbs-engine.service              # systemd unit
```

## Quick start (local dev)

**1. Clone and copy env file:**

```bash
git clone <repo>
cd sbs-engine
cp .env.example .env.development
# edit .env.development with your values (or keep defaults for local Docker MongoDB)
```

**2. Start MongoDB:**

```bash
make docker-run
```

This brings up the `mongo:latest` container defined in [`docker-compose.yml`](docker-compose.yml), using the credentials from `.env.development`.

**3. Run the API:**

```bash
make run                  # loads .env.development, runs cmd/api/main.go
# or, for hot-reload:
make watch                # uses air; will offer to install it if missing
```

The server listens on `http://localhost:${PORT}` (8080 by default).

**4. Try it:**

```bash
curl http://localhost:8080/api/health
curl http://localhost:8080/api/volumes
open http://localhost:8080/swagger/index.html
```

## Configuration

All configuration is via environment variables, loaded from `.env.<environment>` by `load-env.sh`. The full list lives in [`.env.example`](.env.example); the most important ones:

| Variable | Purpose |
|---|---|
| `PORT` | HTTP port (default `8080`) |
| `APP_ENV` | `development` / `staging` / `production` — influences logging and which `.env.*` is loaded |
| `BLUEPRINT_DB_HOST/PORT/NAME` | MongoDB connection target |
| `BLUEPRINT_DB_USERNAME/ROOT_PASSWORD` | MongoDB credentials (omit for unauthenticated local Mongo) |
| `MONGO_USE_SRV` | `true` for MongoDB Atlas (`mongodb+srv://`), default plain `mongodb://` |
| `CORS_ALLOWED_ORIGINS` | Comma-separated origins allowed for write methods. GETs are open. |
| `TRUSTED_PROXY_CIDRS` | When fronted by ALB / CloudFront, the proxy's source CIDRs so `X-Forwarded-For` is honoured |
| `RATE_LIMIT_RPS`, `RATE_LIMIT_BURST` | Per-IP token-bucket defaults |
| `BODY_LIMIT_BYTES` | Maximum request-body size for write methods |
| `ENABLE_SWAGGER` | Set to `true` to expose `/swagger/` (off in production by default) |
| `ADMIN_API_KEY` | Required for write methods (POST/PUT/PATCH/DELETE). Empty = writes refused with 503. See [Authentication](#authentication). |

Three pre-shaped env files are tracked as templates: `.env.development`, `.env.staging`, `.env.production`. The actual env files used at runtime are git-ignored.

## Authentication

Reads (`GET`, `HEAD`, `OPTIONS`) are public. **Mutations (`POST`, `PUT`, `PATCH`, `DELETE`) require an admin API key.** The check is enforced by [`middleware.RequireAPIKey`](internal/middleware/auth.go) and uses constant-time comparison to defeat timing attacks.

Set `ADMIN_API_KEY` in the environment, then send it on every write request as either header:

```bash
# Preferred (matches the Swagger UI "Authorize" button)
curl -X POST http://localhost:8080/api/volumes \
  -H "Authorization: Bearer $ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{ "id": 1, "volume_number": 1, "image_url": "v1.jpg", "total_sbs": 0, "total_languages": 1 }'

# Alternate
curl -X DELETE http://localhost:8080/api/volumes/1 \
  -H "X-API-Key: $ADMIN_API_KEY"
```

Generate a strong key:

```bash
openssl rand -hex 32
```

If `ADMIN_API_KEY` is empty/unset, the middleware **fails closed** — every write returns `503 Service Unavailable` with `"admin API key is not configured"`. This prevents a misconfigured production deploy from accepting anonymous mutations. Wrong or missing key on a write returns `401 Unauthorized`.

In Swagger UI, click **Authorize** and paste `Bearer <ADMIN_API_KEY>` to make the "Try it out" buttons work for write endpoints.

## API surface

Routes are mounted under `/api/`. Full schemas live in the Swagger UI at `/swagger/index.html`. The 🔒 marker in the tables below indicates an endpoint requires `Authorization: Bearer $ADMIN_API_KEY`.

### Volumes

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/volumes` | List all volumes |
| GET | `/api/volumes/paginated?page=&limit=` | Paginated list (max `limit` 100) |
| GET | `/api/volumes/{id}` | Single volume |
| POST | `/api/volumes` | Create 🔒 |
| PUT | `/api/volumes/{id}` | Replace 🔒 |
| PATCH | `/api/volumes/{id}` | Partial update (validated field allowlist) 🔒 |
| DELETE | `/api/volumes/{id}` | Delete 🔒 |

### Sermons / Books

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/app-volume-list/{volume_number}?lang=` | All books in a volume for a language |
| GET | `/api/volumes/{volume_number}/sermons/{sbs_number}?lang=` | Single sermon by location |
| GET | `/api/sermons/search?q=&lang=` | Full-text search over title + content |
| GET | `/api/sermons/random?lang=` | One random sermon |
| POST | `/api/sermons` | Create 🔒 |
| PATCH | `/api/sermons/{object_id}` | Partial update (title / quote / content / image_url) 🔒 |
| DELETE | `/api/sermons/{object_id}` | Delete 🔒 |

### Utility / metadata

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/health` | Liveness + DB ping |
| GET | `/api/stats` | Total volumes, sermons, languages |
| GET | `/api/languages` | Supported language registry |
| GET | `/api/donate` | Donation URL |
| GET | `/api/message-qoutes` | Message quotes (general endpoint) |
| GET | `/swagger/` | Swagger UI (when `ENABLE_SWAGGER=true`) |

### Response envelope

Every JSON response uses the standard envelope from [`internal/response`](internal/response/):

```json
{
  "success": true,
  "message": "...",
  "data":    { ... },
  "error":   "..."
}
```

`error` is set on failure responses; `data` is set on success.

## Languages

Defined in [`internal/database/language.go`](internal/database/language.go):

| Code | Name | Internal ID |
|---|---|---|
| `en` | English | 1 |
| `de` | Deutsch | 2 |

To add a language: extend `supportedLanguages` and `availableLanguages` in `language.go` and re-seed the `books` collection with documents bearing the new `id`.

## Testing

Two suites, separated by Go build tag:

```bash
make test                 # unit tests — handlers (mock service), validators, helpers
make itest                # integration tests — spins up an ephemeral MongoDB via testcontainers-go
```

Integration tests require Docker. Unit tests have no external dependencies.

The mock service pattern lives in [`internal/server/routes_test.go`](internal/server/routes_test.go) — it implements `database.Service` so handlers can be exercised without a real database.

## Building and deploying

### Local binary

```bash
make build                # produces ./main for the host OS
make build-linux          # produces ./main-linux for amd64 Linux (EC2 target)
```

### EC2 deployment flow

```bash
make build-linux          # cross-compile
./deploy.sh production    # bundles main-linux + .env.production + load-env.sh into ./deploy/
# scp the contents of ./deploy/ to the EC2 host, then on the host:
chmod +x main-linux
./ec2-setup.sh            # installs systemd unit, enables, starts the service
```

The systemd unit ([`sbs-engine.service`](sbs-engine.service)) runs the binary as `ec2-user`, restarts on crash with a 5-second delay, and pipes logs to the journal:

```bash
journalctl -u sbs-engine -f       # follow logs on the EC2 host
sudo systemctl restart sbs-engine
```

### Swagger regeneration

Whenever you change `// @...` annotations in handlers:

```bash
swag init -g internal/server/server.go --output docs
```

Commit the regenerated `docs/` folder so the binary serves the latest spec.

## Useful Make targets

| Target | What it does |
|---|---|
| `make all` | Build + test |
| `make build` / `make build-linux` | Compile for host / for EC2 |
| `make run` / `run-staging` / `run-prod` | Run with the matching `.env.<env>` |
| `make docker-run` / `docker-down` | Local MongoDB lifecycle |
| `make test` / `make itest` | Unit / integration suites |
| `make watch` | Hot reload via `air` |
| `make clean` | Remove built binary |

## Contributing

Contributions are welcome. The project is small enough that there's no formal process — but a few conventions keep the history clean and reviews fast.

### 1. Set up your dev environment

```bash
git clone <repo>
cd sbs-engine
cp .env.example .env.development
make docker-run                 # local MongoDB
make watch                      # or: make run
```

If you're touching tests that need a real database, you'll also need Docker for `make itest` (it uses `testcontainers-go` to spin up an ephemeral Mongo).

### 2. Branch from `main`

```bash
git checkout main
git pull
git checkout -b feat/short-description
```

Branch-name prefixes used in this repo:

| Prefix | When to use |
|---|---|
| `feat/` | New endpoint, feature, or capability |
| `fix/` | Bug fix |
| `chore/` | Dependency bumps, refactor without behaviour change, tooling |
| `docs/` | README, Swagger annotations, comments only |

Keep the slug short and kebab-cased, e.g. `feat/sermon-favorites`, `fix/cors-empty-origin`.

### 3. Make focused changes

- One logical change per PR. If you're adding a feature and noticing unrelated cleanup, open a separate PR for the cleanup.
- Match the existing patterns documented in [`.claude/skills/SKILL.md`](.claude/skills/SKILL.md) — that file is the source of truth for "how do we do X here?"
- Add or update tests in the same PR. Handler changes → unit tests in [`internal/server/routes_test.go`](internal/server/routes_test.go) using the mock service. Database changes → integration tests in `internal/database/*_test.go` (build tag `//go:build integration`).
- If you add or change Swagger annotations (`// @Summary`, `// @Router`, etc.), regenerate the spec:

  ```bash
  swag init -g internal/server/server.go --output docs
  ```

  Commit the updated `docs/` folder in the same PR.

### 4. Run the checks before pushing

```bash
go vet ./...
make test
make itest                      # if you touched the database layer
go build ./...                  # the build must be clean
```

### 5. Commit message style

The repo uses [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(volumes): add bulk-import endpoint
fix(rate-limit): honour X-Forwarded-For from trusted proxy
chore: bump mongo-driver to v1.17.5
docs: regenerate Swagger after sermons-search update
```

The scope (in parentheses) is optional but helps when scrolling `git log`. Reference an issue number in the body if one exists.

### 6. Open the pull request

- **Title:** the same Conventional Commits line that goes on the squash commit.
- **Description:** what changed, why, and a short test plan (the actual commands you ran). Link related issues.
- **Checklist** — confirm in the PR description:
  - [ ] `make test` passes
  - [ ] `make itest` passes (if database layer changed)
  - [ ] Swagger regenerated (if annotations changed)
  - [ ] No new env vars without a corresponding entry in `.env.example`
  - [ ] No secrets committed (`.env`, credentials, API keys)

### What's in scope

Especially welcome:

- New read endpoints, language packs, search improvements
- Test coverage for paths the existing suite doesn't reach
- Docs corrections
- Performance fixes with a reproduction (benchmark or load-test output)

Discuss first via an issue before starting:

- Anything that changes the `database.Service` interface (mocks need to update too)
- Anything that breaks the API response envelope (`success`/`message`/`data`/`error`)
- Authentication / authorization — the API is intentionally unauthenticated; gating it changes the deployment model
- Replacing or upgrading core dependencies (Go version, Mongo driver, swag)

### Reporting bugs / security issues

For functional bugs, open a GitHub issue with reproduction steps, expected vs actual behaviour, and which env you saw it on.

For **security** issues, do not open a public issue. Email the maintainer directly with details and a suggested fix or PoC.

## Notes for production

- The API is **unauthenticated**. Treat all `POST` / `PUT` / `PATCH` / `DELETE` routes as administrative — restrict access at the network layer (security group, ALB rules, CloudFront geo-restriction) and the CORS allowlist.
- The Swagger UI is gated behind `ENABLE_SWAGGER=true` so production hosts don't advertise routes to scanners.
- Connection credentials and origin allowlists are environment-driven; never commit `.env.production`.
