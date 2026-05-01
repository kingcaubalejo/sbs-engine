// Package server provides the HTTP server and route definitions for the SBS Engine API.
//
//	@title			SBS Engine API
//	@version		1.0
//	@description	REST API for the SBS (Spiritual Building Stones) Engine service.
//	@description	Reads (GET) are public. Mutations (POST/PUT/PATCH/DELETE) require an admin API key sent as `Authorization: Bearer <ADMIN_API_KEY>`.
//	@host			localhost:8080
//	@BasePath		/api
//
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Provide as: `Bearer <ADMIN_API_KEY>`
package server

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"sbs-engine/internal/database"
)

type Server struct {
	port int

	db     database.Service
	caches *resourceCaches
}

func NewServer() *http.Server {
	port, _ := strconv.Atoi(os.Getenv("PORT"))
	if port == 0 {
		port = 8080
	}
	db := database.NewDatabase()
	NewServer := &Server{
		port:   port,
		db:     db,
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
