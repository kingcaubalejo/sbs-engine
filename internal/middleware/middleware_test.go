package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestRateLimit_PerIPIsolated(t *testing.T) {
	// Two IPs share the same path. The first IP should burn through
	// its budget while the second IP — first request — must succeed.
	mw := RateLimit(RateLimitConfig{
		Default: Bucket{RPS: rate.Limit(0.001), Burst: 2},
	})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	send := func(ip string) int {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = ip + ":12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}

	if c := send("1.1.1.1"); c != http.StatusOK {
		t.Fatalf("first request from IP1 should pass; got %d", c)
	}
	if c := send("1.1.1.1"); c != http.StatusOK {
		t.Fatalf("second request from IP1 should pass (burst=2); got %d", c)
	}
	if c := send("1.1.1.1"); c != http.StatusTooManyRequests {
		t.Fatalf("third request from IP1 should 429; got %d", c)
	}
	// A different IP must still get through — the limiter is per-IP,
	// not global.
	if c := send("2.2.2.2"); c != http.StatusOK {
		t.Fatalf("first request from IP2 should pass; got %d", c)
	}
}

func TestRateLimit_BypassesHealth(t *testing.T) {
	mw := RateLimit(RateLimitConfig{
		Default: Bucket{RPS: rate.Limit(0.001), Burst: 1},
		Bypass:  []string{"/health"},
	})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = "1.1.1.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("/health request %d should bypass rate limit; got %d", i, w.Code)
		}
	}
}

func TestRecover_Returns500(t *testing.T) {
	handler := Recover(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 from recovered panic; got %d", w.Code)
	}
}

func TestBodyLimit_RejectsLargeBody(t *testing.T) {
	mw := BodyLimit(100)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		_, err := buf.ReadFrom(r.Body)
		if err != nil {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader(strings.Repeat("a", 200))
	req := httptest.NewRequest(http.MethodPost, "/", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Errorf("oversize body should be rejected; got %d", w.Code)
	}
}

func TestCacheHeaders_ETagAnd304(t *testing.T) {
	cfg := DefaultCacheConfig()
	mw := CacheHeaders(cfg)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	// First request: receive ETag.
	req := httptest.NewRequest(http.MethodGet, "/api/volumes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected ETag header on GET; got none")
	}
	if w.Header().Get("Cache-Control") == "" {
		t.Fatalf("expected Cache-Control header")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d", w.Code)
	}

	// Second request with If-None-Match: receive 304.
	req2 := httptest.NewRequest(http.MethodGet, "/api/volumes", nil)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotModified {
		t.Errorf("expected 304 with matching If-None-Match; got %d", w2.Code)
	}
}

func TestCacheHeaders_WriteIsNoStore(t *testing.T) {
	mw := CacheHeaders(DefaultCacheConfig())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/volumes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("expected no-store on POST; got %q", w.Header().Get("Cache-Control"))
	}
}

func TestRequestID_HeaderRoundTrip(t *testing.T) {
	var seen string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if seen == "" {
		t.Errorf("expected request id in context")
	}
	if w.Header().Get("X-Request-ID") != seen {
		t.Errorf("expected X-Request-ID header to match context value")
	}
}

func TestRequestID_RespectsClientHeader(t *testing.T) {
	const supplied = "my-trace-id-123"
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestIDFrom(r.Context()); got != supplied {
			t.Errorf("expected client-supplied id passed through; got %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", supplied)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
}

// ---------------------------------------------------------------------------
// RequireAuth — basic shape. JWT-specific cases (valid/expired/tampered)
// live in auth_jwt_test.go.
// ---------------------------------------------------------------------------

// GET, HEAD, and OPTIONS are public; the verifier is never consulted
// for them, so RequireAuth(nil) must let them through.
func TestAuth_GETIsPublic(t *testing.T) {
	handler := RequireAuth(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET should be public; got %d", w.Code)
	}
}

// When no verifier is configured (JWT_SECRET unset at startup) every
// write fails closed with 503. This is the deliberate misconfiguration
// guard: a deploy that forgot to set JWT_SECRET must not silently
// accept anonymous writes.
func TestAuth_FailsClosedWhenUnconfigured(t *testing.T) {
	handler := RequireAuth(nil)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("inner handler must not be reached when verifier is nil")
	}))

	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	req.Header.Set("Authorization", "Bearer anything")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when verifier is nil; got %d", w.Code)
	}
}

// Missing Authorization header on a write should produce 401 with a
// WWW-Authenticate hint so HTTP clients (and the spec) know what
// credential type to send. Token-shape cases are covered in
// auth_jwt_test.go because they need a real Issuer.
func TestAuth_WriteWithoutTokenIsUnauthorized(t *testing.T) {
	// We pass a non-nil verifier here only so RequireAuth doesn't
	// short-circuit on 503. The token check runs independently.
	svc, _ := newTestAuth(t, time.Hour)
	handler := RequireAuth(svc)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("inner handler must not be reached without a token")
	}))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401; got %d", w.Code)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header on 401")
	}
}
