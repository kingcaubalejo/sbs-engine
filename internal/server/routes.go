package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"

	_ "sbs-engine/docs"
	"sbs-engine/internal/database"
	"sbs-engine/internal/response"

	httpSwagger "github.com/swaggo/http-swagger"
)

func (s *Server) RegisterRoutes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", s.HelloWorldHandler)
	mux.HandleFunc("GET /health", s.healthHandler)
	mux.HandleFunc("GET /app-volume-list/{volume_number}", s.getBooksByVolumeHandler)
	mux.HandleFunc("GET /volumes", s.getVolumes)
	mux.HandleFunc("POST /volumes", s.createVolume)
	mux.HandleFunc("PUT /volumes/{id}", s.updateVolume)
	mux.HandleFunc("PATCH /volumes/{id}", s.patchVolume)
	mux.HandleFunc("DELETE /volumes/{id}", s.deleteVolume)
	mux.HandleFunc("GET /message-qoutes", s.messageQoutesHandler)
	mux.HandleFunc("GET /donate", s.donationHandler)

	// Volume extensions
	mux.HandleFunc("GET /volumes/paginated", s.getVolumesPaginated) // must be before wildcard
	mux.HandleFunc("GET /volumes/{id}", s.getVolumeByID)

	// Sermon endpoints — fixed paths registered before wildcard
	mux.HandleFunc("GET /sermons/search", s.searchSermons)
	mux.HandleFunc("GET /sermons/random", s.getRandomSermon)
	mux.HandleFunc("POST /sermons", s.createSermon)
	mux.HandleFunc("DELETE /sermons/{object_id}", s.deleteSermon)
	mux.HandleFunc("PATCH /sermons/{object_id}", s.patchSermon)
	mux.HandleFunc("GET /volumes/{volume_number}/sermons/{sbs_number}", s.getSermonByLocation)

	// Utility
	mux.HandleFunc("GET /stats", s.getStats)
	mux.HandleFunc("GET /languages", s.getLanguages)

	v1 := http.NewServeMux()
	v1.Handle("/api/", http.StripPrefix("/api", mux))

	final := http.NewServeMux()
	final.Handle("/swagger/", httpSwagger.WrapHandler)
	final.Handle("/", s.corsMiddleware(s.rateLimitMiddleware(v1, 1, 2)))

	handler := http.Handler(final)

	return handler
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


// HelloWorldHandler godoc
//	@Summary		Hello World
//	@Tags			general
//	@Produce		json
//	@Success		200	{object}	map[string]string
//	@Router			/ [get]
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

// healthHandler godoc
//	@Summary		Health check
//	@Tags			general
//	@Produce		json
//	@Success		200	{object}	map[string]interface{}
//	@Router			/health [get]
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

// getBooksByVolumeHandler godoc
//	@Summary		Get books by volume
//	@Tags			books
//	@Produce		json
//	@Param			volume_number	path		int		true	"Volume number"
//	@Param			lang			query		string	false	"Language code (default: en)"
//	@Success		200	{object}	response.APIResponse
//	@Router			/app-volume-list/{volume_number} [get]
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

// getVolumes godoc
//	@Summary		List all volumes
//	@Tags			volumes
//	@Produce		json
//	@Success		200	{object}	response.APIResponse
//	@Router			/volumes [get]
func (s *Server) getVolumes(w http.ResponseWriter, r *http.Request) {
	volumes := s.db.GetVolumes()
	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "List of volumes", volumes)
}

// messageQoutesHandler godoc
//	@Summary		Message quotes
//	@Tags			general
//	@Produce		json
//	@Success		200	{object}	map[string]interface{}
//	@Router			/message-qoutes [get]
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

// donationHandler godoc
//	@Summary		Get donation URL
//	@Tags			general
//	@Produce		json
//	@Success		200	{object}	response.APIResponse
//	@Router			/donate [get]
func (s *Server) donationHandler(w http.ResponseWriter, r *http.Request) {
	donate := s.db.GetDonation()

	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Paypal donate redirect url", donate)
}

func validateVolumeCreate(volume struct {
	ID             int    `json:"id"`
	VolumeNumber   int    `json:"volume_number"`
	ImageURL       string `json:"image_url"`
	TotalSBS       int    `json:"total_sbs"`
	TotalLanguages int    `json:"total_languages"`
}) []string {
	var errors []string
	if volume.ID <= 0 {
		errors = append(errors, "id must be greater than 0")
	}
	if volume.VolumeNumber <= 0 {
		errors = append(errors, "volume_number must be greater than 0")
	}
	if strings.TrimSpace(volume.ImageURL) == "" {
		errors = append(errors, "image_url is required")
	}
	if volume.TotalSBS < 0 {
		errors = append(errors, "total_sbs must be non-negative")
	}
	if volume.TotalLanguages < 0 {
		errors = append(errors, "total_languages must be non-negative")
	}
	return errors
}

func validateVolumeUpdate(volume struct {
	ID             int    `json:"id"`
	VolumeNumber   int    `json:"volume_number"`
	ImageURL       string `json:"image_url"`
	TotalSBS       int    `json:"total_sbs"`
	TotalLanguages int    `json:"total_languages"`
}) []string {
	return validateVolumeCreate(volume)
}

func validateVolumePatch(updates map[string]interface{}) []string {
	var errors []string
	for key, value := range updates {
		switch key {
		case "id":
			if id, ok := value.(float64); !ok || id <= 0 {
				errors = append(errors, "id must be a positive number")
			}
		case "volume_number":
			if vn, ok := value.(float64); !ok || vn <= 0 {
				errors = append(errors, "volume_number must be a positive number")
			}
		case "image_url":
			if url, ok := value.(string); !ok || strings.TrimSpace(url) == "" {
				errors = append(errors, "image_url must be a non-empty string")
			}
		case "total_sbs":
			if sbs, ok := value.(float64); !ok || sbs < 0 {
				errors = append(errors, "total_sbs must be a non-negative number")
			}
		case "total_languages":
			if lang, ok := value.(float64); !ok || lang < 0 {
				errors = append(errors, "total_languages must be a non-negative number")
			}
		default:
			errors = append(errors, fmt.Sprintf("unknown field: %s", key))
		}
	}
	return errors
}

func validateID(id int) []string {
	var errors []string
	if id <= 0 {
		errors = append(errors, "id must be greater than 0")
	}
	return errors
}

// createVolume godoc
//	@Summary		Create a volume
//	@Tags			volumes
//	@Accept			json
//	@Produce		json
//	@Param			body	body		database.Volume	true	"Volume payload"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Router			/volumes [post]
func (s *Server) createVolume(w http.ResponseWriter, r *http.Request) {
	var volume struct {
		ID             int    `json:"id"`
		VolumeNumber   int    `json:"volume_number"`
		ImageURL       string `json:"image_url"`
		TotalSBS       int    `json:"total_sbs"`
		TotalLanguages int    `json:"total_languages"`
	}

	if err := json.NewDecoder(r.Body).Decode(&volume); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if errors := validateVolumeCreate(volume); len(errors) > 0 {
		response.Error(w, http.StatusBadRequest, strings.Join(errors, ", "))
		return
	}

	newVolume := s.db.CreateVolume(database.Volume{
		ID:             volume.ID,
		VolumeNumber:   volume.VolumeNumber,
		ImageURL:       volume.ImageURL,
		TotalSBS:       volume.TotalSBS,
		TotalLanguages: volume.TotalLanguages,
	})

	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Volume created successfully", newVolume)
}

// updateVolume godoc
//	@Summary		Replace a volume
//	@Tags			volumes
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int				true	"Volume ID"
//	@Param			body	body		database.Volume	true	"Volume payload"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Router			/volumes/{id} [put]
func (s *Server) updateVolume(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	
	if errors := validateID(id); len(errors) > 0 {
		response.Error(w, http.StatusBadRequest, strings.Join(errors, ", "))
		return
	}
	
	var volume struct {
		ID             int    `json:"id"`
		VolumeNumber   int    `json:"volume_number"`
		ImageURL       string `json:"image_url"`
		TotalSBS       int    `json:"total_sbs"`
		TotalLanguages int    `json:"total_languages"`
	}

	if err := json.NewDecoder(r.Body).Decode(&volume); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if errors := validateVolumeUpdate(volume); len(errors) > 0 {
		response.Error(w, http.StatusBadRequest, strings.Join(errors, ", "))
		return
	}

	updatedVolume := s.db.UpdateVolume(id, database.Volume{
		ID:             volume.ID,
		VolumeNumber:   volume.VolumeNumber,
		ImageURL:       volume.ImageURL,
		TotalSBS:       volume.TotalSBS,
		TotalLanguages: volume.TotalLanguages,
	})

	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Volume updated successfully", updatedVolume)
}

// patchVolume godoc
//	@Summary		Partially update a volume
//	@Tags			volumes
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int						true	"Volume ID"
//	@Param			body	body		map[string]interface{}	true	"Fields to update"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Router			/volumes/{id} [patch]
func (s *Server) patchVolume(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	
	if errors := validateID(id); len(errors) > 0 {
		response.Error(w, http.StatusBadRequest, strings.Join(errors, ", "))
		return
	}
	
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if len(updates) == 0 {
		response.Error(w, http.StatusBadRequest, "No fields to update")
		return
	}

	if errors := validateVolumePatch(updates); len(errors) > 0 {
		response.Error(w, http.StatusBadRequest, strings.Join(errors, ", "))
		return
	}

	updatedVolume := s.db.PatchVolume(id, updates)

	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Volume patched successfully", updatedVolume)
}

// deleteVolume godoc
//	@Summary		Delete a volume
//	@Tags			volumes
//	@Produce		json
//	@Param			id	path		int	true	"Volume ID"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Router			/volumes/{id} [delete]
func (s *Server) deleteVolume(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	
	if errors := validateID(id); len(errors) > 0 {
		response.Error(w, http.StatusBadRequest, strings.Join(errors, ", "))
		return
	}
	
	deleted := s.db.DeleteVolume(id)
	if !deleted {
		response.Error(w, http.StatusNotFound, "Volume not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Volume deleted successfully", nil)
}

// ---------------------------------------------------------------------------
// Volume extensions
// ---------------------------------------------------------------------------

// getVolumeByID godoc
//
//	@Summary		Get a single volume
//	@Tags			volumes
//	@Produce		json
//	@Param			id	path		int	true	"Volume ID"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Router			/volumes/{id} [get]
func (s *Server) getVolumeByID(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	if errors := validateID(id); len(errors) > 0 {
		response.Error(w, http.StatusBadRequest, strings.Join(errors, ", "))
		return
	}

	volume, found := s.db.GetVolumeByID(id)
	if !found {
		response.Error(w, http.StatusNotFound, "Volume not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Volume retrieved", volume)
}

// getVolumesPaginated godoc
//
//	@Summary		List volumes with pagination
//	@Tags			volumes
//	@Produce		json
//	@Param			page	query		int	false	"Page number (default 1)"
//	@Param			limit	query		int	false	"Items per page (default 10, max 100)"
//	@Success		200	{object}	response.APIResponse
//	@Router			/volumes/paginated [get]
func (s *Server) getVolumesPaginated(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	items, total := s.db.GetVolumesPaginated(page, limit)

	pages := total / int64(limit)
	if total%int64(limit) != 0 {
		pages++
	}

	result := database.PaginatedVolumes{
		Items: items,
		Total: total,
		Page:  page,
		Limit: limit,
		Pages: pages,
	}

	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Paginated list of volumes", result)
}

// ---------------------------------------------------------------------------
// Sermon handlers
// ---------------------------------------------------------------------------

// getSermonByLocation godoc
//
//	@Summary		Get a specific sermon by volume and SBS number
//	@Tags			sermons
//	@Produce		json
//	@Param			volume_number	path		int		true	"Volume number"
//	@Param			sbs_number		path		int		true	"SBS (sermon) number"
//	@Param			lang			query		string	false	"Language code (default: en)"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Router			/volumes/{volume_number}/sermons/{sbs_number} [get]
func (s *Server) getSermonByLocation(w http.ResponseWriter, r *http.Request) {
	volumeNumber, _ := strconv.Atoi(r.PathValue("volume_number"))
	sbsNumber, _ := strconv.Atoi(r.PathValue("sbs_number"))
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "en"
	}

	if volumeNumber <= 0 || sbsNumber <= 0 {
		response.Error(w, http.StatusBadRequest, "volume_number and sbs_number must be positive integers")
		return
	}

	sermon, found := s.db.GetSermonByLocation(volumeNumber, sbsNumber, lang)
	if !found {
		response.Error(w, http.StatusNotFound, "Sermon not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Sermon retrieved", sermon)
}

// searchSermons godoc
//
//	@Summary		Search sermons by keyword
//	@Tags			sermons
//	@Produce		json
//	@Param			q		query		string	true	"Search keyword"
//	@Param			lang	query		string	false	"Language code (default: en)"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Router			/sermons/search [get]
func (s *Server) searchSermons(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "en"
	}

	if query == "" {
		response.Error(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	sermons := s.db.SearchSermons(query, lang)
	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Search results", sermons)
}

// getRandomSermon godoc
//
//	@Summary		Get a random sermon
//	@Tags			sermons
//	@Produce		json
//	@Param			lang	query		string	false	"Language code (default: en)"
//	@Success		200	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Router			/sermons/random [get]
func (s *Server) getRandomSermon(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "en"
	}

	sermon, found := s.db.GetRandomSermon(lang)
	if !found {
		response.Error(w, http.StatusNotFound, "No sermons available")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Random sermon", sermon)
}

func validateSermon(s database.Sermon) []string {
	var errors []string
	if s.SbsNumber <= 0 {
		errors = append(errors, "sbs_number must be greater than 0")
	}
	if s.VolumeNumber <= 0 {
		errors = append(errors, "volume_number must be greater than 0")
	}
	if strings.TrimSpace(s.Title) == "" {
		errors = append(errors, "title is required")
	}
	if s.ID <= 0 {
		errors = append(errors, "id (language) must be greater than 0")
	}
	return errors
}

// createSermon godoc
//
//	@Summary		Create a new sermon
//	@Tags			sermons
//	@Accept			json
//	@Produce		json
//	@Param			body	body		database.Sermon	true	"Sermon payload"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Router			/sermons [post]
func (s *Server) createSermon(w http.ResponseWriter, r *http.Request) {
	var sermon database.Sermon
	if err := json.NewDecoder(r.Body).Decode(&sermon); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if errors := validateSermon(sermon); len(errors) > 0 {
		response.Error(w, http.StatusBadRequest, strings.Join(errors, ", "))
		return
	}

	created := s.db.CreateSermon(sermon)
	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Sermon created successfully", created)
}

var allowedSermonPatchFields = map[string]bool{
	"title":     true,
	"quote":     true,
	"content":   true,
	"image_url": true,
}

func validateSermonPatch(updates map[string]interface{}) []string {
	var errors []string
	for key, value := range updates {
		if !allowedSermonPatchFields[key] {
			errors = append(errors, fmt.Sprintf("unknown field: %s", key))
			continue
		}
		if str, ok := value.(string); !ok || strings.TrimSpace(str) == "" {
			errors = append(errors, fmt.Sprintf("%s must be a non-empty string", key))
		}
	}
	return errors
}

// deleteSermon godoc
//
//	@Summary		Delete a sermon by ObjectID
//	@Tags			sermons
//	@Produce		json
//	@Param			object_id	path		string	true	"MongoDB ObjectID hex"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Router			/sermons/{object_id} [delete]
func (s *Server) deleteSermon(w http.ResponseWriter, r *http.Request) {
	objectID := r.PathValue("object_id")
	if objectID == "" {
		response.Error(w, http.StatusBadRequest, "object_id is required")
		return
	}

	deleted := s.db.DeleteSermon(objectID)
	if !deleted {
		response.Error(w, http.StatusNotFound, "Sermon not found or invalid ID")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Sermon deleted successfully", nil)
}

// patchSermon godoc
//
//	@Summary		Partially update a sermon
//	@Tags			sermons
//	@Accept			json
//	@Produce		json
//	@Param			object_id	path		string					true	"MongoDB ObjectID hex"
//	@Param			body		body		map[string]interface{}	true	"Fields to update"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Failure		404	{object}	response.APIResponse
//	@Router			/sermons/{object_id} [patch]
func (s *Server) patchSermon(w http.ResponseWriter, r *http.Request) {
	objectID := r.PathValue("object_id")
	if objectID == "" {
		response.Error(w, http.StatusBadRequest, "object_id is required")
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		response.Error(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if len(updates) == 0 {
		response.Error(w, http.StatusBadRequest, "No fields to update")
		return
	}

	if errors := validateSermonPatch(updates); len(errors) > 0 {
		response.Error(w, http.StatusBadRequest, strings.Join(errors, ", "))
		return
	}

	sermon, found := s.db.PatchSermon(objectID, updates)
	if !found {
		response.Error(w, http.StatusNotFound, "Sermon not found or invalid ID")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Sermon patched successfully", sermon)
}

// ---------------------------------------------------------------------------
// Utility handlers
// ---------------------------------------------------------------------------

// getStats godoc
//
//	@Summary		Get platform statistics
//	@Tags			general
//	@Produce		json
//	@Success		200	{object}	response.APIResponse
//	@Router			/stats [get]
func (s *Server) getStats(w http.ResponseWriter, r *http.Request) {
	stats := s.db.GetStats()
	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Platform statistics", stats)
}

// getLanguages godoc
//
//	@Summary		List supported languages
//	@Tags			general
//	@Produce		json
//	@Success		200	{object}	response.APIResponse
//	@Router			/languages [get]
func (s *Server) getLanguages(w http.ResponseWriter, r *http.Request) {
	languages := s.db.GetLanguages()
	w.Header().Set("Content-Type", "application/json")
	response.Success(w, "Available languages", languages)
}