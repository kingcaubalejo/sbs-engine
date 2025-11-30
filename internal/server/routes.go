package server

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"
	"fmt"

	"golang.org/x/time/rate"

	"sbs-engine/internal/response"
)

func (s *Server) RegisterRoutes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", s.HelloWorldHandler)
	mux.HandleFunc("GET /health", s.healthHandler)
	mux.HandleFunc("GET /app-volume-list/{volume_number}", s.getBooksByVolumeHandler)
	mux.HandleFunc("GET /volumes", s.getVolumes)
	
	mux.HandleFunc("GET /message-qoutes", s.messageQoutesHandler)

	mux.HandleFunc("GET /donate", s.donationHandler)

	v1 := http.NewServeMux()
	v1.Handle("/api/", http.StripPrefix("/api", mux))
	
	handler := s.rateLimitMiddleware(v1, 1, 2)
	handler = s.corsMiddleware(handler)

	return handler;
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {

	allowedOrigins := map[string]bool{
		"http://13.229.210.18": true,
		"http://localhost:8080": true,
		"https://yourdomain.com": true,
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if !allowedOrigins[origin] {
			http.Error(w, "CORS origin not allowed", http.StatusForbidden)
			return
		}
		
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")
		w.Header().Set("Access-Control-Allow-Credentials", "false")

		// Handle preflight OPTIONS requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Proceed with the next handler
		next.ServeHTTP(w, r)
	})
}

func (s *Server) rateLimitMiddleware(next http.Handler, r rate.Limit, burst int) http.Handler {
	limiter := rate.NewLimiter(r, burst)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		log.Printf("[Info] Ip Address= %v", ip)

		if err != nil {
			http.Error(w, "Error fetching Address", http.StatusTooManyRequests)
			return
		}

		if !limiter.Allow() {

			reservation := limiter.Reserve()
			delay := reservation.Delay()

			if delay <= 0 {
				delay = time.Second
			}

			w.Header().Set("Retry-After", fmt.Sprintf("%.0f", delay.Seconds()))
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}


func (s *Server) HelloWorldHandler(w http.ResponseWriter, r *http.Request) {
	resp := map[string]string{"message": "Hello World"}
	jsonResp, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "Failed to marshal response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(jsonResp); err != nil {
		log.Printf("Failed to write response: %v", err)
	}
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	resp, err := json.Marshal(s.db.HealthCheck())
	if err != nil {
		http.Error(w, "Failed to marshal health check response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(resp); err != nil {
		log.Printf("Failed to write response: %v", err)
	}
}

func (s *Server) getBooksByVolumeHandler(w http.ResponseWriter, r *http.Request) {
	language := r.URL.Query().Get("lang")
	volumeNumber, _ := strconv.Atoi(r.PathValue("volume_number"))

	if language ==  "" {
		language = "en"
	}

	books := s.db.GetBooksByVolume(volumeNumber, language)

	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "List of books per volume", books)
}

func (s *Server) getVolumes(w http.ResponseWriter, r *http.Request) {
	volumes := s.db.GetVolumes()
	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "List of volumes", volumes)
}

func (s *Server) messageQoutesHandler(w http.ResponseWriter, r *http.Request) {
	resp, err := json.Marshal(s.db.HealthCheck())
	if err != nil {
		http.Error(w, "Failed to marshal health check response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(resp); err != nil {
		log.Printf("Failed to write response: %v", err)
	}
}

func (s *Server) donationHandler(w http.ResponseWriter, r *http.Request) {
	donate := s.db.GetDonation()

	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Paypal donate redirect url", donate)
}