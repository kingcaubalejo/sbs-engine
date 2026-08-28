package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"

	_ "sbs-engine/docs"
	"sbs-engine/internal/cache"
	"sbs-engine/internal/database"
	"sbs-engine/internal/middleware"
	"sbs-engine/internal/response"

	httpSwagger "github.com/swaggo/http-swagger"
)

// searchQueryMaxLen rejects abusive search inputs at the handler before
// they reach Mongo. 100 characters comfortably covers any reasonable
// natural-language query and stops a pathological "qqqqq..." input
// from forcing the database to score thousands of partial matches.
const searchQueryMaxLen = 100

// searchQueryMinLen rejects single-character searches that effectively
// touch every document.
const searchQueryMinLen = 2

// resourceCaches groups the per-resource TTL caches that protect read
// endpoints from repeated database hits. They are short-lived and keyed
// by query (for endpoints with parameters) or a constant (for
// parameterless endpoints). On write the relevant cache entries are
// removed so callers see fresh data immediately.
type resourceCaches struct {
	stats    *cache.TTLCache[string, database.Stats]
	langs    *cache.TTLCache[string, []database.Language]
	volumes  *cache.TTLCache[string, []database.Volume]
	volByID  *cache.TTLCache[int, database.Volume]
	donate   *cache.TTLCache[string, database.Donation]
}

func newResourceCaches() *resourceCaches {
	return &resourceCaches{
		stats:   cache.NewTTL[string, database.Stats](),
		langs:   cache.NewTTL[string, []database.Language](),
		volumes: cache.NewTTL[string, []database.Volume](),
		volByID: cache.NewTTL[int, database.Volume](),
		donate:  cache.NewTTL[string, database.Donation](),
	}
}

func (s *Server) RegisterRoutes() http.Handler {
	if s.caches == nil {
		s.caches = newResourceCaches()
	}

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

	mux.HandleFunc("GET /volumes/paginated", s.getVolumesPaginated)
	mux.HandleFunc("GET /volumes/{id}", s.getVolumeByID)

	mux.HandleFunc("GET /sermons/search", s.searchSermons)
	mux.HandleFunc("GET /sermons/random", s.getRandomSermon)
	mux.HandleFunc("POST /sermons", s.createSermon)
	mux.HandleFunc("DELETE /sermons/{object_id}", s.deleteSermon)
	mux.HandleFunc("PATCH /sermons/{object_id}", s.patchSermon)
	mux.HandleFunc("GET /volumes/{volume_number}/sermons/{sbs_number}", s.getSermonByLocation)

	mux.HandleFunc("GET /stats", s.getStats)
	mux.HandleFunc("GET /languages", s.getLanguages)

	mux.HandleFunc("POST /auth/login", s.loginHandler)
	mux.HandleFunc("POST /auth/register", s.registerHandler)

	v1 := http.NewServeMux()
	v1.Handle("/api/", http.StripPrefix("/api", mux))
	v1.HandleFunc("GET /robots.txt", robotsHandler)

	final := http.NewServeMux()
	if os.Getenv("ENABLE_SWAGGER") == "true" {
		final.Handle("/swagger/", httpSwagger.WrapHandler)
	}
	final.Handle("/", s.buildMiddlewareChain(v1))

	return final
}

// buildMiddlewareChain wires the request pipeline. Order matters:
//
//	Recover (outermost — catches panics in any inner middleware or handler)
//	  └─ SecurityHeaders (cheap, always-on response headers)
//	    └─ RequestID (must precede AccessLog so log lines have an ID)
//	      └─ AccessLog (one structured line per request)
//	        └─ CORS (preflight handled before rate-limit so OPTIONS is free)
//	          └─ RateLimit (per-IP + per-route — runs BEFORE auth so a
//	                       brute-force attempt on the credentials is throttled)
//	            └─ RequireAuth (gates non-GET requests on a JWT issued by
//	                            /auth/login. POST /auth/login is bypassed so
//	                            it can mint tokens. Fails closed with 503
//	                            if JWT_SECRET is not configured.)
//	              └─ BodyLimit (only applies to write methods)
//	                └─ Gzip (compresses below the cache layer so cached bytes
//	                         are uncompressed and ETag matches the resource)
//	                  └─ CacheHeaders (ETag + Cache-Control)
//	                    └─ handler
func (s *Server) buildMiddlewareChain(next http.Handler) http.Handler {
	rps, burst := rateLimitFromEnv()

	chain := next
	chain = middleware.CacheHeaders(middleware.DefaultCacheConfig())(chain)
	chain = middleware.Gzip(chain)
	chain = middleware.BodyLimit(bodyLimitBytes())(chain)
	chain = middleware.RequireAuth(s.auth)(chain)
	chain = middleware.RateLimit(middleware.RateLimitConfig{
		Default: middleware.Bucket{RPS: rps, Burst: burst},
		PerRoute: map[string]middleware.Bucket{
			"/api/sermons/search": {RPS: rate.Limit(2), Burst: 10},
			"/api/sermons/random": {RPS: rate.Limit(5), Burst: 15},
		},
		Bypass: []string{"/health", "/api/health", "/swagger/", "/robots.txt"},
	})(chain)
	chain = middleware.CORS(chain)
	chain = middleware.AccessLog(chain)
	chain = middleware.RequestID(chain)
	chain = middleware.SecurityHeaders(chain)
	chain = middleware.Recover(chain)
	chain = middleware.APIVersionMiddleware(chain)
	return chain
}

func rateLimitFromEnv() (rate.Limit, int) {
	rps := 20.0
	burst := 40
	if v := os.Getenv("RATE_LIMIT_RPS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			rps = f
		}
	}
	if v := os.Getenv("RATE_LIMIT_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			burst = n
		}
	}
	return rate.Limit(rps), burst
}

func bodyLimitBytes() int64 {
	const defaultLimit int64 = 64 << 10
	if v := os.Getenv("BODY_LIMIT_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return defaultLimit
}

func robotsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

// HelloWorldHandler godoc
//
//	@Summary		Hello World
//	@Tags			general
//	@Produce		json
//	@Success		200	{object}	map[string]string
//	@Router			/ [get]
func (s *Server) HelloWorldHandler(w http.ResponseWriter, _ *http.Request) {
	body, _ := json.Marshal(map[string]string{"message": "Hello World"})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// healthHandler godoc
//
//	@Summary		Health check
//	@Tags			general
//	@Produce		json
//	@Success		200	{object}	map[string]interface{}
//	@Router			/health [get]
func (s *Server) healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.db.HealthCheck())
}

// getBooksByVolumeHandler godoc
//
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

	if language == "" {
		language = "en"
	}

	books := s.db.GetBooksByVolume(volumeNumber, language)
	response.Success(w, "List of books per volume", books)
}

// getVolumes godoc
//
//	@Summary		List all volumes
//	@Tags			volumes
//	@Produce		json
//	@Success		200	{object}	response.APIResponse
//	@Router			/volumes [get]
func (s *Server) getVolumes(w http.ResponseWriter, r *http.Request) {
	const key = "all"
	const ttl = 5 * time.Minute
	apiVersion := middleware.GetAPIVersion(r)

	volumes, ok := s.caches.volumes.Get(key)
	if !ok {
		volumes = s.db.GetVolumes()
		s.caches.volumes.Set(key, volumes, ttl)
	}

	if apiVersion == "1" {
		fmt.Println("legacy code should be read here")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
	
		_ = json.NewEncoder(w).Encode(volumes)
	} 
	
	if apiVersion == "2"  {
		response.Success(w, "List of volumes", volumes)
	}

}

// messageQoutesHandler godoc
//
//	@Summary		Message quotes
//	@Tags			general
//	@Produce		json
//	@Success		200	{object}	response.APIResponse
//	@Router			/message-qoutes [get]
func (s *Server) messageQoutesHandler(w http.ResponseWriter, _ *http.Request) {
	response.Success(w, "Quotes", []string{
		"In the beginning was the Word, and the Word was with God.",
		"For God so loved the world.",
		"The Lord is my shepherd; I shall not want.",
	})
}

// donationHandler godoc
//
//	@Summary		Get donation URL
//	@Tags			general
//	@Produce		json
//	@Success		200	{object}	response.APIResponse
//	@Router			/donate [get]
func (s *Server) donationHandler(w http.ResponseWriter, _ *http.Request) {
	const key = "default"
	const ttl = time.Hour

	donate, ok := s.caches.donate.Get(key)
	if !ok {
		donate = s.db.GetDonation()
		s.caches.donate.Set(key, donate, ttl)
	}
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

// invalidateVolumeCaches drops cached volume responses after a write so
// the next read hits the database. Cheaper than recomputing every entry
// and avoids serving stale data after a create/update/delete.
func (s *Server) invalidateVolumeCaches(id int) {
	s.caches.volumes.Remove("all")
	if id > 0 {
		s.caches.volByID.Remove(id)
	}
}

// createVolume godoc
//
//	@Summary		Create a volume
//	@Tags			volumes
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		database.Volume	true	"Volume payload"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Failure		401	{object}	response.APIResponse
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
	s.invalidateVolumeCaches(newVolume.ID)

	response.Success(w, "Volume created successfully", newVolume)
}

// updateVolume godoc
//
//	@Summary		Replace a volume
//	@Tags			volumes
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int				true	"Volume ID"
//	@Param			body	body		database.Volume	true	"Volume payload"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Failure		401	{object}	response.APIResponse
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
	s.invalidateVolumeCaches(id)

	response.Success(w, "Volume updated successfully", updatedVolume)
}

// patchVolume godoc
//
//	@Summary		Partially update a volume
//	@Tags			volumes
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int						true	"Volume ID"
//	@Param			body	body		map[string]interface{}	true	"Fields to update"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Failure		401	{object}	response.APIResponse
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
	s.invalidateVolumeCaches(id)

	response.Success(w, "Volume patched successfully", updatedVolume)
}

// deleteVolume godoc
//
//	@Summary		Delete a volume
//	@Tags			volumes
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		int	true	"Volume ID"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Failure		401	{object}	response.APIResponse
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
	s.invalidateVolumeCaches(id)

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

	const ttl = 5 * time.Minute
	if v, ok := s.caches.volByID.Get(id); ok {
		response.Success(w, "Volume retrieved", v)
		return
	}

	volume, found := s.db.GetVolumeByID(id)
	if !found {
		response.Error(w, http.StatusNotFound, "Volume not found")
		return
	}
	s.caches.volByID.Set(id, volume, ttl)

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

	if len(query) < searchQueryMinLen {
		response.Error(w, http.StatusBadRequest, fmt.Sprintf("q must be at least %d characters", searchQueryMinLen))
		return
	}
	if len(query) > searchQueryMaxLen {
		response.Error(w, http.StatusBadRequest, fmt.Sprintf("q must be at most %d characters", searchQueryMaxLen))
		return
	}

	sermons := s.db.SearchSermons(query, lang)
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
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		database.Sermon	true	"Sermon payload"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Failure		401	{object}	response.APIResponse
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
//	@Security		BearerAuth
//	@Produce		json
//	@Param			object_id	path		string	true	"MongoDB ObjectID hex"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Failure		401	{object}	response.APIResponse
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

	response.Success(w, "Sermon deleted successfully", nil)
}

// patchSermon godoc
//
//	@Summary		Partially update a sermon
//	@Tags			sermons
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			object_id	path		string					true	"MongoDB ObjectID hex"
//	@Param			body		body		map[string]interface{}	true	"Fields to update"
//	@Success		200	{object}	response.APIResponse
//	@Failure		400	{object}	response.APIResponse
//	@Failure		401	{object}	response.APIResponse
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
func (s *Server) getStats(w http.ResponseWriter, _ *http.Request) {
	const key = "all"
	const ttl = 5 * time.Minute

	stats, ok := s.caches.stats.Get(key)
	if !ok {
		stats = s.db.GetStats()
		s.caches.stats.Set(key, stats, ttl)
	}
	response.Success(w, "Platform statistics", stats)
}

// getLanguages godoc
//
//	@Summary		List supported languages
//	@Tags			general
//	@Produce		json
//	@Success		200	{object}	response.APIResponse
//	@Router			/languages [get]
func (s *Server) getLanguages(w http.ResponseWriter, _ *http.Request) {
	const key = "all"
	const ttl = time.Hour

	langs, ok := s.caches.langs.Get(key)
	if !ok {
		langs = s.db.GetLanguages()
		s.caches.langs.Set(key, langs, ttl)
	}
	response.Success(w, "Available languages", langs)
}
