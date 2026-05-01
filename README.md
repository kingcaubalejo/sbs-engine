# sbs-engine

A REST API that serves the **SBS (Spiritual Building Stones / Sermon By Sermon)** content library — volumes, books, sermons, and supporting metadata — to web and mobile clients.

**Reads are public.** Anyone can fetch volumes, sermons, and metadata via `GET` without credentials.

**Writes are authenticated.** `POST` / `PUT` / `PATCH` / `DELETE` require a JWT obtained from `POST /api/auth/login` with email + password, sent as `Authorization: Bearer <jwt>`. See [Authentication](#authentication).

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
│   ├── auth/                       # password hashing, JWT issue/verify, Service
│   │   ├── service.go              # Login, CreateUser, Verify, AdminCount
│   │   ├── token.go                # JWT Issuer (HS256) + Claims
│   │   ├── password.go             # bcrypt cost=12, strength check
│   │   ├── user.go                 # User struct + UserStore interface
│   │   └── errors.go               # sentinel errors
│   ├── database/                   # MongoDB layer
│   │   ├── connection.go           # client setup, Service interface, HealthCheck
│   │   ├── volume.go               # volumes CRUD
│   │   ├── book.go, sermon_ops.go  # sermons / books CRUD + search
│   │   ├── stats.go, donation.go   # ancillary endpoints
│   │   ├── language.go             # supported language registry
│   │   ├── user.go                 # auth.UserStore implementation
│   │   ├── indexes.go              # programmatic index creation on startup
│   │   └── *_test.go               # unit + integration tests (testcontainers)
│   ├── cache/                      # generic + TTL caches (utility package)
│   ├── middleware/                 # cross-cutting concerns (CORS, rate limit, gzip, ETag, auth, logging…)
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
| `JWT_SECRET` | **Required.** HMAC-HS256 signing secret for user JWTs. Empty disables `/auth/login` and writes return 503. |
| `JWT_TTL` | Token lifetime (Go duration, e.g. `24h`). Default `24h` if unset. |
| `BOOTSTRAP_ADMIN_EMAIL` | First-run admin seed (only used if no admin user exists yet). |
| `BOOTSTRAP_ADMIN_PASSWORD` | Password for the bootstrap admin. Used once, then ignored. |

> If `JWT_SECRET` is empty, every write returns `503 Service Unavailable` — the auth middleware fails closed so a misconfigured deploy can't silently accept anonymous mutations.

Three pre-shaped env files are tracked as templates: `.env.development`, `.env.staging`, `.env.production`. The actual env files used at runtime are git-ignored.

## Authentication

Reads (`GET`, `HEAD`, `OPTIONS`) and the login endpoint (`POST /api/auth/login`) are public. Every other method requires a JWT, enforced by [`middleware.RequireAuth`](internal/middleware/auth.go).

The JWT is HS256-signed using `JWT_SECRET` and carries `uid`, `email`, `is_admin` plus standard `iat` / `exp` / `nbf` claims. Default lifetime 24 h, configurable via `JWT_TTL`. The signing-method is pinned in [`auth.Issuer.Verify`](internal/auth/token.go) so an attacker cannot downgrade to `none` or RS256-confuse the verifier.

Clients send the token as:

```
Authorization: Bearer <jwt>
```

If `JWT_SECRET` is unset, the middleware fails closed (503). If the caller sends no token on a write, 401 with `WWW-Authenticate: Bearer realm="sbs-engine"`. Invalid or expired token, also 401.

### User auth flow

**1. Seed the first admin** (once, via env vars on first startup):

```bash
# .env.development
JWT_SECRET=$(openssl rand -hex 32)
JWT_TTL=24h
BOOTSTRAP_ADMIN_EMAIL=admin@example.com
BOOTSTRAP_ADMIN_PASSWORD=please-change-me-now
```

The server creates this user on boot if and only if no admin exists yet ([`server.go` `bootstrapAdmin`](internal/server/server.go)). After the first run you can clear those two env vars; they are read once.

**2. Log in to get a token:**

```bash
curl -s -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"please-change-me-now"}' | jq
```

Response:

```json
{
  "success": true,
  "message": "login successful",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "user": { "id": "...", "email": "admin@example.com", "is_admin": true, "created_at": "..." }
  }
}
```

Failures collapse to a single `401 invalid email or password` so account existence isn't leaked. Bcrypt timing on misses is matched to hits via a dummy compare ([`auth.Service.Login`](internal/auth/service.go)).

**3. Use the token on writes:**

```bash
TOKEN=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...

curl -X POST http://localhost:8080/api/volumes \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{ "id": 1, "volume_number": 1, "image_url": "v1.jpg", "total_sbs": 0, "total_languages": 1 }'
```

### Creating additional users (admin only)

Once an admin exists, more accounts are created via `POST /api/auth/register`. This endpoint is gated by `RequireAuth` and the handler additionally checks the caller's JWT carries `is_admin: true`. Open self-registration is intentionally not exposed.

```bash
TOKEN=...   # admin JWT from /auth/login

curl -i -X POST http://localhost:8080/api/auth/register \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "email": "editor@example.com",
    "password": "at-least-eight-chars",
    "is_admin": false
  }'
```

Successful response (`201 Created`):

```json
{
  "success": true,
  "message": "user created",
  "data": { "id": "...", "email": "editor@example.com", "is_admin": false, "created_at": "..." }
}
```

Status-code map:

| Code | Cause |
|---|---|
| `201` | User created |
| `400` | Invalid JSON, missing/empty fields, malformed email, password under 8 chars |
| `401` | No / invalid credentials on the request |
| `403` | Caller authenticated as a non-admin user |
| `409` | Email already exists |
| `503` | `JWT_SECRET` not configured (auth service is nil) |

Registration **does not** auto-issue a token — the new user must `POST /auth/login` to get one. Email is normalized (trimmed and lowercased) before storage; the unique index on `users.email` rejects duplicates.

### Backend / CI / cron callers

Machine clients use the same login flow as humans — give them a dedicated user account (e.g. `ci@example.com`) and have them call `/auth/login` to obtain a JWT, then refresh on expiry. This keeps every action attributable to a real account so audits work, and lets you disable a compromised credential by flipping that user's password without touching every other caller.

For long-running automation, set `JWT_TTL` long enough that re-login isn't paged-on hourly (24 h is the default), or implement a periodic re-login in the client.

### Swagger UI

Click **Authorize** in `/swagger/index.html` and paste `Bearer <jwt>` into the value field. Get the JWT from `POST /auth/login` first.

### Public web / mobile clients should not embed credentials

A long-lived admin JWT does not belong in a client binary or browser bundle — extraction takes minutes. The recommended topology for a public app that needs writes:

```
public app  →  your admin BFF (holds creds, runs your own user auth)  →  sbs-engine
```

The admin BFF is the only thing that talks to `/api/volumes` writes. Public app talks only to GETs. See the architecture guidance for the full pattern.

## API surface

Routes are mounted under `/api/`. Full schemas live in the Swagger UI at `/swagger/index.html`. The 🔒 marker indicates the endpoint requires a JWT obtained from `POST /auth/login`, sent as `Authorization: Bearer <jwt>`.

### Auth

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/auth/login` | Exchange email + password for a JWT |
| POST | `/api/auth/register` | Create a new user account 🔒 (admin only) |

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
- Authentication / authorization — changes to credential handling, JWT claims shape, or the gating policy on routes
- Replacing or upgrading core dependencies (Go version, Mongo driver, swag, JWT library)

### Reporting bugs / security issues

For functional bugs, open a GitHub issue with reproduction steps, expected vs actual behaviour, and which env you saw it on.

For **security** issues, do not open a public issue. Email the maintainer directly with details and a suggested fix or PoC.

## Notes for production

- **Reads are public; writes require a JWT.** `JWT_SECRET` must be set in `.env.production` — an empty value fails closed with 503 on every write.
- **Always run behind TLS** (CloudFront / ALB). Bearer tokens travel in plaintext otherwise; an on-path attacker captures them once.
- **`JWT_SECRET` rotation** invalidates every issued token immediately. Plan for a brief re-login window and avoid rotating during traffic spikes.
- **Bootstrap admin variables (`BOOTSTRAP_ADMIN_EMAIL` / `BOOTSTRAP_ADMIN_PASSWORD`) are first-run only.** Once an admin exists they are ignored. Remove them from `.env.production` after the first deploy so the password isn't sitting in your environment.
- **Compromised user account** → change that user's password (or delete the row), then rotate `JWT_SECRET` if you suspect any issued tokens are in attacker hands.
- **The Swagger UI is gated behind `ENABLE_SWAGGER=true`** so production hosts don't advertise routes to scanners.
- **Restrict the network layer** in addition to the auth layer: security groups, ALB rules, CloudFront geo-restriction. Auth is one layer of defence, not the only one.
- **Connection credentials and origin allowlists are environment-driven**; never commit `.env.production`.
