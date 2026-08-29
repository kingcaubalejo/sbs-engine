// Package server provides the HTTP server and route definitions for the SBS Engine API.
//
//	@title			SBS Engine API
//	@version		1.0
//	@description	REST API for the SBS (Spiritual Building Stones) Engine service.
//	@description	Reads (GET) are public. POST /auth/login is public. All other writes (POST/PUT/PATCH/DELETE) require a JWT obtained from /auth/login, sent as `Authorization: Bearer <jwt>`.
//	@host			localhost:8080
//	@BasePath		/api
//
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				JWT from POST /auth/login. Format: `Bearer <jwt>`.
package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"sbs-engine/internal/auth"
	"sbs-engine/internal/database"
)

type Server struct {
	port int

	db     database.Service
	auth   *auth.Service
	caches *resourceCaches
}

func NewServer() *http.Server {
	port, _ := strconv.Atoi(os.Getenv("PORT"))
	if port == 0 {
		port = 8080
	}
	db := database.NewDatabase()

	// Auth is optional at startup. If JWT_SECRET is unset we keep the
	// static-key-only path working; the login endpoint refuses with 503
	// until the secret is configured.
	authSvc := buildAuthService(db)
	if authSvc != nil {
		bootstrapAdmin(authSvc)
	}

	NewServer := &Server{
		port:   port,
		db:     db,
		auth:   authSvc,
		caches: newResourceCaches(),
	}

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", NewServer.port),
		Handler:           NewServer.RegisterRoutes(),
		IdleTimeout:       time.Minute,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}

	return server
}

// buildAuthService wires the auth package against the live MongoDB
// store. Returns nil (and logs) when JWT_SECRET is unset so the rest
// of the server still boots — useful in environments that only need
// the legacy static-key path.
func buildAuthService(db *database.Database) *auth.Service {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		slog.Warn("JWT_SECRET not set; login endpoint disabled, static API key remains active")
		return nil
	}
	ttl, _ := time.ParseDuration(os.Getenv("JWT_TTL"))
	issuer, err := auth.NewIssuer(secret, ttl)
	if err != nil {
		slog.Error("failed to build JWT issuer; login disabled", "error", err)
		return nil
	}
	return auth.NewService(db, issuer)
}

// bootstrapAdmin seeds an initial admin user if none exists yet, so
// the very first deploy isn't locked out. Driven by env so secrets
// stay out of source control. No-op if the env vars are unset or any
// admin already exists.
func bootstrapAdmin(svc *auth.Service) {
	email := os.Getenv("BOOTSTRAP_ADMIN_EMAIL")
	password := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if email == "" || password == "" {
		return
	}
	count, err := svc.AdminCount()
	if err != nil {
		slog.Warn("admin count check failed; skipping bootstrap", "error", err)
		return
	}
	if count > 0 {
		return
	}
	if _, err := svc.CreateUser(email, password, true); err != nil {
		slog.Error("admin bootstrap failed", "error", err)
		return
	}
	slog.Info("bootstrap admin created", "email", email)
}
