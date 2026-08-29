package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"sbs-engine/internal/database"
	"sbs-engine/internal/middleware"
	"sbs-engine/internal/response"
)

// ---------------------------------------------------------------------------
// Mock service
// ---------------------------------------------------------------------------

type mockService struct {
	volumes            []database.Volume
	sermons            []database.Sermon
	donation           database.Donation
	stats              database.Stats
	languages          []database.Language
	deleteVolumeReturn bool
	deleteSermonReturn bool
	volumeFound        bool
	sermonFound        bool
	pagedVolumes       []database.Volume
	volumeTotal        int64
}

// Volume methods
func (m *mockService) GetVolumes() []database.Volume { return m.volumes }
func (m *mockService) GetVolumeByID(_ int) (database.Volume, bool) {
	if m.volumeFound && len(m.volumes) > 0 {
		return m.volumes[0], true
	}
	return database.Volume{}, false
}
func (m *mockService) GetVolumesPaginated(_ int, _ int) ([]database.Volume, int64) {
	return m.pagedVolumes, m.volumeTotal
}
func (m *mockService) CreateVolume(v database.Volume) database.Volume {
	m.volumes = append(m.volumes, v)
	return v
}
func (m *mockService) UpdateVolume(_ int, v database.Volume) database.Volume { return v }
func (m *mockService) PatchVolume(id int, _ map[string]interface{}) database.Volume {
	return database.Volume{ID: id}
}
func (m *mockService) DeleteVolume(_ int) bool { return m.deleteVolumeReturn }

// Sermon methods
func (m *mockService) GetBooksByVolume(_ int, _ string) []database.Sermon { return m.sermons }
func (m *mockService) GetSermonByLocation(_ int, _ int, _ string) (database.Sermon, bool) {
	if m.sermonFound && len(m.sermons) > 0 {
		return m.sermons[0], true
	}
	return database.Sermon{}, false
}
func (m *mockService) SearchSermons(_ string, _ string) []database.Sermon { return m.sermons }
func (m *mockService) GetRandomSermon(_ string) (database.Sermon, bool) {
	if m.sermonFound && len(m.sermons) > 0 {
		return m.sermons[0], true
	}
	return database.Sermon{}, false
}
func (m *mockService) CreateSermon(s database.Sermon) database.Sermon {
	m.sermons = append(m.sermons, s)
	return s
}
func (m *mockService) DeleteSermon(_ string) bool { return m.deleteSermonReturn }
func (m *mockService) PatchSermon(_ string, _ map[string]interface{}) (database.Sermon, bool) {
	if m.sermonFound && len(m.sermons) > 0 {
		return m.sermons[0], true
	}
	return database.Sermon{}, false
}

// Utility methods
func (m *mockService) HealthCheck() map[string]string {
	return map[string]string{"status": "up", "message": "Database is healthy"}
}
func (m *mockService) GetDonation() database.Donation  { return m.donation }
func (m *mockService) GetStats() database.Stats        { return m.stats }
func (m *mockService) GetLanguages() []database.Language { return m.languages }

func newTestServer(mock *mockService) *Server {
	return &Server{db: mock, caches: newResourceCaches()}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func decodeBody(t *testing.T, body io.Reader) response.APIResponse {
	t.Helper()
	var resp response.APIResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// HelloWorldHandler
// ---------------------------------------------------------------------------

func TestHandler(t *testing.T) {
	s := &Server{}
	server := httptest.NewServer(http.HandlerFunc(s.HelloWorldHandler))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200; got %v", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	expected := `{"message":"Hello World"}`
	if string(body) != expected {
		t.Errorf("expected %q; got %q", expected, string(body))
	}
}

// ---------------------------------------------------------------------------
// healthHandler
// ---------------------------------------------------------------------------

func TestHealthHandler(t *testing.T) {
	s := newTestServer(&mockService{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	s.healthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if result["status"] != "up" {
		t.Errorf("expected status 'up'; got %q", result["status"])
	}
	if result["message"] != "Database is healthy" {
		t.Errorf("unexpected message: %q", result["message"])
	}
}

// ---------------------------------------------------------------------------
// getVolumes
// ---------------------------------------------------------------------------

func TestGetVolumes_ReturnsList(t *testing.T) {
	mock := &mockService{
		volumes: []database.Volume{
			{ID: 1, VolumeNumber: 1, ImageURL: "v1.jpg", TotalSBS: 5, TotalLanguages: 2},
			{ID: 2, VolumeNumber: 2, ImageURL: "v2.jpg", TotalSBS: 10, TotalLanguages: 3},
		},
	}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodGet, "/volumes", nil)
	w := httptest.NewRecorder()

	s.getVolumes(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}

	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true")
	}

	data, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be an array, got %T", resp.Data)
	}
	if len(data) != 2 {
		t.Errorf("expected 2 volumes; got %d", len(data))
	}
}

func TestGetVolumes_Empty(t *testing.T) {
	s := newTestServer(&mockService{volumes: []database.Volume{}})
	req := httptest.NewRequest(http.MethodGet, "/volumes", nil)
	w := httptest.NewRecorder()

	s.getVolumes(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// createVolume
// ---------------------------------------------------------------------------

func TestCreateVolume_Valid(t *testing.T) {
	s := newTestServer(&mockService{})
	payload := `{"id":1,"volume_number":1,"image_url":"cover.jpg","total_sbs":10,"total_languages":2}`
	req := httptest.NewRequest(http.MethodPost, "/volumes", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.createVolume(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true; error=%v", resp.Error)
	}
}

func TestCreateVolume_InvalidJSON(t *testing.T) {
	s := newTestServer(&mockService{})
	req := httptest.NewRequest(http.MethodPost, "/volumes", bytes.NewBufferString("{bad json}"))
	w := httptest.NewRecorder()

	s.createVolume(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if resp.Success {
		t.Error("expected success=false")
	}
}

func TestCreateVolume_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr string
	}{
		{
			name:    "zero id",
			payload: `{"id":0,"volume_number":1,"image_url":"x.jpg","total_sbs":1,"total_languages":1}`,
			wantErr: "id must be greater than 0",
		},
		{
			name:    "zero volume_number",
			payload: `{"id":1,"volume_number":0,"image_url":"x.jpg","total_sbs":1,"total_languages":1}`,
			wantErr: "volume_number must be greater than 0",
		},
		{
			name:    "empty image_url",
			payload: `{"id":1,"volume_number":1,"image_url":"","total_sbs":1,"total_languages":1}`,
			wantErr: "image_url is required",
		},
		{
			name:    "negative total_sbs",
			payload: `{"id":1,"volume_number":1,"image_url":"x.jpg","total_sbs":-1,"total_languages":1}`,
			wantErr: "total_sbs must be non-negative",
		},
		{
			name:    "negative total_languages",
			payload: `{"id":1,"volume_number":1,"image_url":"x.jpg","total_sbs":1,"total_languages":-1}`,
			wantErr: "total_languages must be non-negative",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(&mockService{})
			req := httptest.NewRequest(http.MethodPost, "/volumes", bytes.NewBufferString(tc.payload))
			w := httptest.NewRecorder()

			s.createVolume(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400; got %d", w.Code)
			}
			resp := decodeBody(t, w.Body)
			if resp.Success {
				t.Error("expected success=false")
			}
			if errStr, ok := resp.Error.(string); !ok || errStr == "" {
				t.Errorf("expected non-empty error string, got %v", resp.Error)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// updateVolume
// ---------------------------------------------------------------------------

func TestUpdateVolume_Valid(t *testing.T) {
	s := newTestServer(&mockService{})
	payload := `{"id":1,"volume_number":1,"image_url":"new.jpg","total_sbs":5,"total_languages":2}`
	req := httptest.NewRequest(http.MethodPut, "/volumes/1", bytes.NewBufferString(payload))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()

	s.updateVolume(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true; error=%v", resp.Error)
	}
}

func TestUpdateVolume_InvalidID(t *testing.T) {
	s := newTestServer(&mockService{})
	req := httptest.NewRequest(http.MethodPut, "/volumes/0", bytes.NewBufferString(`{}`))
	req.SetPathValue("id", "0")
	w := httptest.NewRecorder()

	s.updateVolume(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400; got %d", w.Code)
	}
}

func TestUpdateVolume_InvalidJSON(t *testing.T) {
	s := newTestServer(&mockService{})
	req := httptest.NewRequest(http.MethodPut, "/volumes/1", bytes.NewBufferString("not-json"))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()

	s.updateVolume(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400; got %d", w.Code)
	}
}

func TestUpdateVolume_ValidationError(t *testing.T) {
	s := newTestServer(&mockService{})
	payload := `{"id":0,"volume_number":1,"image_url":"x.jpg","total_sbs":1,"total_languages":1}`
	req := httptest.NewRequest(http.MethodPut, "/volumes/1", bytes.NewBufferString(payload))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()

	s.updateVolume(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400; got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// patchVolume
// ---------------------------------------------------------------------------

func TestPatchVolume_Valid(t *testing.T) {
	s := newTestServer(&mockService{})
	payload := `{"image_url":"updated.jpg"}`
	req := httptest.NewRequest(http.MethodPatch, "/volumes/1", bytes.NewBufferString(payload))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()

	s.patchVolume(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true; error=%v", resp.Error)
	}
}

func TestPatchVolume_InvalidID(t *testing.T) {
	s := newTestServer(&mockService{})
	req := httptest.NewRequest(http.MethodPatch, "/volumes/0", bytes.NewBufferString(`{"image_url":"x.jpg"}`))
	req.SetPathValue("id", "0")
	w := httptest.NewRecorder()

	s.patchVolume(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400; got %d", w.Code)
	}
}

func TestPatchVolume_InvalidJSON(t *testing.T) {
	s := newTestServer(&mockService{})
	req := httptest.NewRequest(http.MethodPatch, "/volumes/1", bytes.NewBufferString("bad"))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()

	s.patchVolume(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400; got %d", w.Code)
	}
}

func TestPatchVolume_EmptyBody(t *testing.T) {
	s := newTestServer(&mockService{})
	req := httptest.NewRequest(http.MethodPatch, "/volumes/1", bytes.NewBufferString(`{}`))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()

	s.patchVolume(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if resp.Success {
		t.Error("expected success=false for empty patch body")
	}
}

func TestPatchVolume_UnknownField(t *testing.T) {
	s := newTestServer(&mockService{})
	payload := `{"unknown_field": "value"}`
	req := httptest.NewRequest(http.MethodPatch, "/volumes/1", bytes.NewBufferString(payload))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()

	s.patchVolume(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown field; got %d", w.Code)
	}
}

func TestPatchVolume_InvalidFieldValue(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"negative id", `{"id": -1}`},
		{"negative volume_number", `{"volume_number": 0}`},
		{"empty image_url", `{"image_url": "  "}`},
		{"negative total_sbs", `{"total_sbs": -5}`},
		{"negative total_languages", `{"total_languages": -1}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(&mockService{})
			req := httptest.NewRequest(http.MethodPatch, "/volumes/1", bytes.NewBufferString(tc.payload))
			req.SetPathValue("id", "1")
			w := httptest.NewRecorder()

			s.patchVolume(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400; got %d", w.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// deleteVolume
// ---------------------------------------------------------------------------

func TestDeleteVolume_Found(t *testing.T) {
	s := newTestServer(&mockService{deleteVolumeReturn: true})
	req := httptest.NewRequest(http.MethodDelete, "/volumes/1", nil)
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()

	s.deleteVolume(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true")
	}
}

func TestDeleteVolume_NotFound(t *testing.T) {
	s := newTestServer(&mockService{deleteVolumeReturn: false})
	req := httptest.NewRequest(http.MethodDelete, "/volumes/99", nil)
	req.SetPathValue("id", "99")
	w := httptest.NewRecorder()

	s.deleteVolume(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if resp.Success {
		t.Error("expected success=false for not-found")
	}
}

func TestDeleteVolume_InvalidID(t *testing.T) {
	s := newTestServer(&mockService{})
	req := httptest.NewRequest(http.MethodDelete, "/volumes/0", nil)
	req.SetPathValue("id", "0")
	w := httptest.NewRecorder()

	s.deleteVolume(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400; got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// getBooksByVolumeHandler
// ---------------------------------------------------------------------------

func TestGetBooksByVolumeHandler_DefaultLang(t *testing.T) {
	mock := &mockService{
		sermons: []database.Sermon{
			{ID: 1, Title: "Sermon 1", VolumeNumber: 1},
		},
	}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodGet, "/app-volume-list/1", nil)
	req.SetPathValue("volume_number", "1")
	w := httptest.NewRecorder()

	s.getBooksByVolumeHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true")
	}
}

func TestGetBooksByVolumeHandler_WithLangParam(t *testing.T) {
	mock := &mockService{
		sermons: []database.Sermon{
			{ID: 2, Title: "Predigt 1", VolumeNumber: 1},
		},
	}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodGet, "/app-volume-list/1?lang=de", nil)
	req.SetPathValue("volume_number", "1")
	w := httptest.NewRecorder()

	s.getBooksByVolumeHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
}

func TestGetBooksByVolumeHandler_Empty(t *testing.T) {
	s := newTestServer(&mockService{sermons: []database.Sermon{}})
	req := httptest.NewRequest(http.MethodGet, "/app-volume-list/99", nil)
	req.SetPathValue("volume_number", "99")
	w := httptest.NewRecorder()

	s.getBooksByVolumeHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// donationHandler
// ---------------------------------------------------------------------------

func TestDonationHandler(t *testing.T) {
	expectedURL := "https://www.sandbox.paypal.com/donate/?hosted_button_id=TEST"
	mock := &mockService{
		donation: database.Donation{Status: 200, Url: expectedURL},
	}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodGet, "/donate", nil)
	w := httptest.NewRecorder()

	s.donationHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true")
	}
}

// ---------------------------------------------------------------------------
// CORS middleware (new policy: open GETs, allowlist for writes)
// ---------------------------------------------------------------------------

func TestCORSMiddleware_GETIsOpen(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := middleware.CORS(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://anywhere.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET should be allowed for any origin; got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://anywhere.example" {
		t.Errorf("expected origin reflected; got %q", got)
	}
}

func TestCORSMiddleware_WriteAllowedOrigin(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := middleware.CORS(inner)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST from allowed origin should pass; got %d", w.Code)
	}
}

func TestCORSMiddleware_WriteDeniedOrigin(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler must not be called for denied write")
	})
	handler := middleware.CORS(inner)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("POST from disallowed origin should return 403; got %d", w.Code)
	}
}

func TestCORSMiddleware_WriteEmptyOriginPasses(t *testing.T) {
	// Empty Origin = non-browser client (curl/server-to-server). The
	// SOP threat model does not apply, so writes are accepted.
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := middleware.CORS(inner)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST with empty Origin should pass; got %d", w.Code)
	}
}

func TestCORSMiddleware_Preflight(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called for OPTIONS preflight")
	})
	handler := middleware.CORS(inner)

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 for preflight; got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// validateVolumeCreate (table-driven)
// ---------------------------------------------------------------------------

type volumeInput struct {
	ID             int
	VolumeNumber   int
	ImageURL       string
	TotalSBS       int
	TotalLanguages int
}

func callValidateCreate(v volumeInput) []string {
	return validateVolumeCreate(struct {
		ID             int    `json:"id"`
		VolumeNumber   int    `json:"volume_number"`
		ImageURL       string `json:"image_url"`
		TotalSBS       int    `json:"total_sbs"`
		TotalLanguages int    `json:"total_languages"`
	}{
		ID:             v.ID,
		VolumeNumber:   v.VolumeNumber,
		ImageURL:       v.ImageURL,
		TotalSBS:       v.TotalSBS,
		TotalLanguages: v.TotalLanguages,
	})
}

func TestValidateVolumeCreate(t *testing.T) {
	tests := []struct {
		name       string
		input      volumeInput
		wantErrors int
	}{
		{"valid", volumeInput{1, 1, "img.jpg", 10, 2}, 0},
		{"zero id", volumeInput{0, 1, "img.jpg", 10, 2}, 1},
		{"zero volume_number", volumeInput{1, 0, "img.jpg", 10, 2}, 1},
		{"empty image_url", volumeInput{1, 1, "", 10, 2}, 1},
		{"whitespace image_url", volumeInput{1, 1, "  ", 10, 2}, 1},
		{"negative total_sbs", volumeInput{1, 1, "img.jpg", -1, 2}, 1},
		{"negative total_languages", volumeInput{1, 1, "img.jpg", 10, -1}, 1},
		{"multiple errors", volumeInput{0, 0, "", -1, -1}, 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := callValidateCreate(tc.input)
			if len(errs) != tc.wantErrors {
				t.Errorf("expected %d error(s); got %d: %v", tc.wantErrors, len(errs), errs)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validateVolumePatch (table-driven)
// ---------------------------------------------------------------------------

func TestValidateVolumePatch(t *testing.T) {
	tests := []struct {
		name       string
		updates    map[string]interface{}
		wantErrors int
	}{
		{"valid image_url", map[string]interface{}{"image_url": "new.jpg"}, 0},
		{"valid id", map[string]interface{}{"id": float64(5)}, 0},
		{"valid volume_number", map[string]interface{}{"volume_number": float64(3)}, 0},
		{"valid total_sbs zero", map[string]interface{}{"total_sbs": float64(0)}, 0},
		{"invalid id zero", map[string]interface{}{"id": float64(0)}, 1},
		{"invalid id negative", map[string]interface{}{"id": float64(-1)}, 1},
		{"invalid volume_number zero", map[string]interface{}{"volume_number": float64(0)}, 1},
		{"empty image_url", map[string]interface{}{"image_url": "  "}, 1},
		{"negative total_sbs", map[string]interface{}{"total_sbs": float64(-1)}, 1},
		{"negative total_languages", map[string]interface{}{"total_languages": float64(-2)}, 1},
		{"unknown field", map[string]interface{}{"ghost": "value"}, 1},
		{"multiple valid fields", map[string]interface{}{"image_url": "x.jpg", "total_sbs": float64(5)}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateVolumePatch(tc.updates)
			if len(errs) != tc.wantErrors {
				t.Errorf("expected %d error(s); got %d: %v", tc.wantErrors, len(errs), errs)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validateID (table-driven)
// ---------------------------------------------------------------------------

func TestValidateID(t *testing.T) {
	tests := []struct {
		id      int
		wantErr bool
	}{
		{1, false},
		{100, false},
		{0, true},
		{-1, true},
		{-99, true},
	}
	for _, tc := range tests {
		errs := validateID(tc.id)
		hasErr := len(errs) > 0
		if hasErr != tc.wantErr {
			t.Errorf("validateID(%d): wantErr=%v; got errs=%v", tc.id, tc.wantErr, errs)
		}
	}
}

// ---------------------------------------------------------------------------
// getVolumeByID (use case 1)
// ---------------------------------------------------------------------------

func TestGetVolumeByID_Found(t *testing.T) {
	mock := &mockService{
		volumes:     []database.Volume{{ID: 3, VolumeNumber: 3, ImageURL: "v3.jpg"}},
		volumeFound: true,
	}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodGet, "/volumes/3", nil)
	req.SetPathValue("id", "3")
	w := httptest.NewRecorder()

	s.getVolumeByID(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true")
	}
}

func TestGetVolumeByID_NotFound(t *testing.T) {
	s := newTestServer(&mockService{volumeFound: false})
	req := httptest.NewRequest(http.MethodGet, "/volumes/99", nil)
	req.SetPathValue("id", "99")
	w := httptest.NewRecorder()

	s.getVolumeByID(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404; got %d", w.Code)
	}
}

func TestGetVolumeByID_InvalidID(t *testing.T) {
	s := newTestServer(&mockService{})
	req := httptest.NewRequest(http.MethodGet, "/volumes/0", nil)
	req.SetPathValue("id", "0")
	w := httptest.NewRecorder()

	s.getVolumeByID(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400; got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// getVolumesPaginated (use case 6)
// ---------------------------------------------------------------------------

func TestGetVolumesPaginated_DefaultParams(t *testing.T) {
	mock := &mockService{
		pagedVolumes: []database.Volume{
			{ID: 1, VolumeNumber: 1},
			{ID: 2, VolumeNumber: 2},
		},
		volumeTotal: 20,
	}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodGet, "/volumes/paginated", nil)
	w := httptest.NewRecorder()

	s.getVolumesPaginated(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true")
	}
}

func TestGetVolumesPaginated_CustomParams(t *testing.T) {
	mock := &mockService{
		pagedVolumes: []database.Volume{{ID: 1}},
		volumeTotal:  50,
	}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodGet, "/volumes/paginated?page=2&limit=5", nil)
	w := httptest.NewRecorder()

	s.getVolumesPaginated(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
}

func TestGetVolumesPaginated_ClampLimit(t *testing.T) {
	// limit > 100 should be clamped to 10
	s := newTestServer(&mockService{pagedVolumes: []database.Volume{}, volumeTotal: 0})
	req := httptest.NewRequest(http.MethodGet, "/volumes/paginated?limit=999", nil)
	w := httptest.NewRecorder()

	s.getVolumesPaginated(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// getSermonByLocation (use case 2)
// ---------------------------------------------------------------------------

func TestGetSermonByLocation_Found(t *testing.T) {
	mock := &mockService{
		sermons:     []database.Sermon{{ID: 1, Title: "Test Sermon", VolumeNumber: 1, SbsNumber: 1}},
		sermonFound: true,
	}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodGet, "/volumes/1/sermons/1", nil)
	req.SetPathValue("volume_number", "1")
	req.SetPathValue("sbs_number", "1")
	w := httptest.NewRecorder()

	s.getSermonByLocation(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true")
	}
}

func TestGetSermonByLocation_NotFound(t *testing.T) {
	s := newTestServer(&mockService{sermonFound: false})
	req := httptest.NewRequest(http.MethodGet, "/volumes/1/sermons/99", nil)
	req.SetPathValue("volume_number", "1")
	req.SetPathValue("sbs_number", "99")
	w := httptest.NewRecorder()

	s.getSermonByLocation(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404; got %d", w.Code)
	}
}

func TestGetSermonByLocation_InvalidParams(t *testing.T) {
	tests := []struct {
		name         string
		volumeNumber string
		sbsNumber    string
	}{
		{"zero volume_number", "0", "1"},
		{"zero sbs_number", "1", "0"},
		{"both zero", "0", "0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(&mockService{})
			req := httptest.NewRequest(http.MethodGet, "/volumes/"+tc.volumeNumber+"/sermons/"+tc.sbsNumber, nil)
			req.SetPathValue("volume_number", tc.volumeNumber)
			req.SetPathValue("sbs_number", tc.sbsNumber)
			w := httptest.NewRecorder()

			s.getSermonByLocation(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400; got %d", w.Code)
			}
		})
	}
}

func TestGetSermonByLocation_WithLangParam(t *testing.T) {
	mock := &mockService{
		sermons:     []database.Sermon{{ID: 2, Title: "Predigt", VolumeNumber: 1}},
		sermonFound: true,
	}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodGet, "/volumes/1/sermons/1?lang=de", nil)
	req.SetPathValue("volume_number", "1")
	req.SetPathValue("sbs_number", "1")
	w := httptest.NewRecorder()

	s.getSermonByLocation(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// searchSermons (use case 3)
// ---------------------------------------------------------------------------

func TestSearchSermons_WithResults(t *testing.T) {
	mock := &mockService{
		sermons: []database.Sermon{
			{ID: 1, Title: "Grace and Faith"},
			{ID: 1, Title: "Faith in Action"},
		},
	}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodGet, "/sermons/search?q=faith", nil)
	w := httptest.NewRecorder()

	s.searchSermons(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true")
	}
}

func TestSearchSermons_EmptyQuery(t *testing.T) {
	s := newTestServer(&mockService{})
	req := httptest.NewRequest(http.MethodGet, "/sermons/search", nil)
	w := httptest.NewRecorder()

	s.searchSermons(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty q; got %d", w.Code)
	}
}

func TestSearchSermons_WhitespaceQuery(t *testing.T) {
	s := newTestServer(&mockService{})
	// spaces must be percent-encoded in the URL; handler trims and treats as empty
	req := httptest.NewRequest(http.MethodGet, "/sermons/search?q=%20%20%20", nil)
	w := httptest.NewRecorder()

	s.searchSermons(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for whitespace q; got %d", w.Code)
	}
}

func TestSearchSermons_WithLang(t *testing.T) {
	s := newTestServer(&mockService{sermons: []database.Sermon{}})
	req := httptest.NewRequest(http.MethodGet, "/sermons/search?q=gnade&lang=de", nil)
	w := httptest.NewRecorder()

	s.searchSermons(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// getStats (use case 4)
// ---------------------------------------------------------------------------

func TestGetStats(t *testing.T) {
	mock := &mockService{
		stats: database.Stats{
			TotalVolumes:   5,
			TotalSermons:   120,
			TotalLanguages: 2,
		},
	}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()

	s.getStats(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true")
	}
	if resp.Data == nil {
		t.Error("expected non-nil data")
	}
}

// ---------------------------------------------------------------------------
// getLanguages (use case 5)
// ---------------------------------------------------------------------------

func TestGetLanguages(t *testing.T) {
	mock := &mockService{
		languages: []database.Language{
			{Code: "en", Name: "English", ID: 1},
			{Code: "de", Name: "Deutsch", ID: 2},
		},
	}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodGet, "/languages", nil)
	w := httptest.NewRecorder()

	s.getLanguages(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true")
	}

	langs, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be array; got %T", resp.Data)
	}
	if len(langs) != 2 {
		t.Errorf("expected 2 languages; got %d", len(langs))
	}
}

// ---------------------------------------------------------------------------
// getRandomSermon (use case 7)
// ---------------------------------------------------------------------------

func TestGetRandomSermon_Found(t *testing.T) {
	mock := &mockService{
		sermons:     []database.Sermon{{ID: 1, Title: "A Random Sermon"}},
		sermonFound: true,
	}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodGet, "/sermons/random", nil)
	w := httptest.NewRecorder()

	s.getRandomSermon(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true")
	}
}

func TestGetRandomSermon_NotFound(t *testing.T) {
	s := newTestServer(&mockService{sermonFound: false})
	req := httptest.NewRequest(http.MethodGet, "/sermons/random", nil)
	w := httptest.NewRecorder()

	s.getRandomSermon(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404; got %d", w.Code)
	}
}

func TestGetRandomSermon_WithLang(t *testing.T) {
	mock := &mockService{
		sermons:     []database.Sermon{{ID: 2, Title: "Zufallspredigt"}},
		sermonFound: true,
	}
	s := newTestServer(mock)
	req := httptest.NewRequest(http.MethodGet, "/sermons/random?lang=de", nil)
	w := httptest.NewRecorder()

	s.getRandomSermon(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// createSermon (use case 8)
// ---------------------------------------------------------------------------

func TestCreateSermon_Valid(t *testing.T) {
	s := newTestServer(&mockService{})
	payload := `{"id":1,"title":"New Sermon","sbs_number":1,"volume_number":1,"book_number":1,"image_url":"s.jpg","content":"Content"}`
	req := httptest.NewRequest(http.MethodPost, "/sermons", bytes.NewBufferString(payload))
	w := httptest.NewRecorder()

	s.createSermon(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true; error=%v", resp.Error)
	}
}

func TestCreateSermon_InvalidJSON(t *testing.T) {
	s := newTestServer(&mockService{})
	req := httptest.NewRequest(http.MethodPost, "/sermons", bytes.NewBufferString("bad json"))
	w := httptest.NewRecorder()

	s.createSermon(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400; got %d", w.Code)
	}
}

func TestCreateSermon_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"zero sbs_number", `{"id":1,"title":"T","sbs_number":0,"volume_number":1}`},
		{"zero volume_number", `{"id":1,"title":"T","sbs_number":1,"volume_number":0}`},
		{"empty title", `{"id":1,"title":"","sbs_number":1,"volume_number":1}`},
		{"zero lang id", `{"id":0,"title":"T","sbs_number":1,"volume_number":1}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(&mockService{})
			req := httptest.NewRequest(http.MethodPost, "/sermons", bytes.NewBufferString(tc.payload))
			w := httptest.NewRecorder()

			s.createSermon(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400; got %d", w.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// deleteSermon (use case 9)
// ---------------------------------------------------------------------------

func TestDeleteSermon_Found(t *testing.T) {
	s := newTestServer(&mockService{deleteSermonReturn: true})
	req := httptest.NewRequest(http.MethodDelete, "/sermons/507f1f77bcf86cd799439011", nil)
	req.SetPathValue("object_id", "507f1f77bcf86cd799439011")
	w := httptest.NewRecorder()

	s.deleteSermon(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true")
	}
}

func TestDeleteSermon_NotFound(t *testing.T) {
	s := newTestServer(&mockService{deleteSermonReturn: false})
	req := httptest.NewRequest(http.MethodDelete, "/sermons/507f1f77bcf86cd799439011", nil)
	req.SetPathValue("object_id", "507f1f77bcf86cd799439011")
	w := httptest.NewRecorder()

	s.deleteSermon(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404; got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// patchSermon (use case 10)
// ---------------------------------------------------------------------------

func TestPatchSermon_Valid(t *testing.T) {
	mock := &mockService{
		sermons:     []database.Sermon{{ID: 1, Title: "Updated Title"}},
		sermonFound: true,
	}
	s := newTestServer(mock)
	payload := `{"title":"Updated Title"}`
	req := httptest.NewRequest(http.MethodPatch, "/sermons/507f1f77bcf86cd799439011", bytes.NewBufferString(payload))
	req.SetPathValue("object_id", "507f1f77bcf86cd799439011")
	w := httptest.NewRecorder()

	s.patchSermon(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200; got %d", w.Code)
	}
	resp := decodeBody(t, w.Body)
	if !resp.Success {
		t.Errorf("expected success=true; error=%v", resp.Error)
	}
}

func TestPatchSermon_NotFound(t *testing.T) {
	s := newTestServer(&mockService{sermonFound: false})
	payload := `{"title":"New Title"}`
	req := httptest.NewRequest(http.MethodPatch, "/sermons/507f1f77bcf86cd799439011", bytes.NewBufferString(payload))
	req.SetPathValue("object_id", "507f1f77bcf86cd799439011")
	w := httptest.NewRecorder()

	s.patchSermon(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404; got %d", w.Code)
	}
}

func TestPatchSermon_InvalidJSON(t *testing.T) {
	s := newTestServer(&mockService{})
	req := httptest.NewRequest(http.MethodPatch, "/sermons/507f1f77bcf86cd799439011", bytes.NewBufferString("bad"))
	req.SetPathValue("object_id", "507f1f77bcf86cd799439011")
	w := httptest.NewRecorder()

	s.patchSermon(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400; got %d", w.Code)
	}
}

func TestPatchSermon_EmptyBody(t *testing.T) {
	s := newTestServer(&mockService{})
	req := httptest.NewRequest(http.MethodPatch, "/sermons/507f1f77bcf86cd799439011", bytes.NewBufferString(`{}`))
	req.SetPathValue("object_id", "507f1f77bcf86cd799439011")
	w := httptest.NewRecorder()

	s.patchSermon(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400; got %d", w.Code)
	}
}

func TestPatchSermon_UnknownField(t *testing.T) {
	s := newTestServer(&mockService{})
	req := httptest.NewRequest(http.MethodPatch, "/sermons/507f1f77bcf86cd799439011", bytes.NewBufferString(`{"sbs_number":5}`))
	req.SetPathValue("object_id", "507f1f77bcf86cd799439011")
	w := httptest.NewRecorder()

	s.patchSermon(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for identity-field patch; got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// validateSermon (table-driven)
// ---------------------------------------------------------------------------

func TestValidateSermon(t *testing.T) {
	tests := []struct {
		name       string
		sermon     database.Sermon
		wantErrors int
	}{
		{"valid", database.Sermon{ID: 1, Title: "Test", SbsNumber: 1, VolumeNumber: 1}, 0},
		{"zero sbs_number", database.Sermon{ID: 1, Title: "Test", SbsNumber: 0, VolumeNumber: 1}, 1},
		{"zero volume_number", database.Sermon{ID: 1, Title: "Test", SbsNumber: 1, VolumeNumber: 0}, 1},
		{"empty title", database.Sermon{ID: 1, Title: "", SbsNumber: 1, VolumeNumber: 1}, 1},
		{"whitespace title", database.Sermon{ID: 1, Title: "  ", SbsNumber: 1, VolumeNumber: 1}, 1},
		{"zero lang id", database.Sermon{ID: 0, Title: "Test", SbsNumber: 1, VolumeNumber: 1}, 1},
		{"all errors", database.Sermon{ID: 0, Title: "", SbsNumber: 0, VolumeNumber: 0}, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateSermon(tc.sermon)
			if len(errs) != tc.wantErrors {
				t.Errorf("expected %d error(s); got %d: %v", tc.wantErrors, len(errs), errs)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validateSermonPatch (table-driven)
// ---------------------------------------------------------------------------

func TestValidateSermonPatch(t *testing.T) {
	tests := []struct {
		name       string
		updates    map[string]interface{}
		wantErrors int
	}{
		{"valid title", map[string]interface{}{"title": "New Title"}, 0},
		{"valid quote", map[string]interface{}{"quote": "A quote"}, 0},
		{"valid content", map[string]interface{}{"content": "Long content here"}, 0},
		{"valid image_url", map[string]interface{}{"image_url": "img.jpg"}, 0},
		{"multiple valid fields", map[string]interface{}{"title": "T", "quote": "Q"}, 0},
		{"empty title", map[string]interface{}{"title": "  "}, 1},
		{"empty image_url", map[string]interface{}{"image_url": ""}, 1},
		{"unknown field sbs_number", map[string]interface{}{"sbs_number": float64(1)}, 1},
		{"unknown field volume_number", map[string]interface{}{"volume_number": float64(1)}, 1},
		{"unknown field id", map[string]interface{}{"id": float64(1)}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateSermonPatch(tc.updates)
			if len(errs) != tc.wantErrors {
				t.Errorf("expected %d error(s); got %d: %v", tc.wantErrors, len(errs), errs)
			}
		})
	}
}
