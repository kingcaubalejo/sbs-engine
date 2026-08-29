package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sbs-engine/internal/auth"
)

// stubStore is the minimum auth.UserStore RequireAuth/Service need to
// build a Service — we only exercise Verify here, so Get/Create/Count
// never run.
type stubStore struct{}

func (stubStore) GetUserByEmail(string) (auth.User, bool, error) {
	return auth.User{}, false, errors.New("not used")
}
func (stubStore) CreateUser(u auth.User) (auth.User, error) { return u, nil }
func (stubStore) CountAdmins() (int64, error)               { return 0, nil }

// newTestAuth returns a Service whose Issuer can mint tokens that
// Verify will accept, and a cleanup-free user struct to issue against.
func newTestAuth(t *testing.T, ttl time.Duration) (*auth.Service, auth.User) {
	t.Helper()
	issuer, err := auth.NewIssuer("test-secret-not-used-anywhere-else", ttl)
	if err != nil {
		t.Fatalf("NewIssuer failed: %v", err)
	}
	svc := auth.NewService(stubStore{}, issuer)
	return svc, auth.User{ID: "u1", Email: "admin@example.com", IsAdmin: true}
}

// A valid token must pass through RequireAuth and land claims on the
// request context so handlers can identify the caller.
func TestAuth_AcceptsValidJWT(t *testing.T) {
	svc, user := newTestAuth(t, time.Hour)
	tok, err := svc.Verify(mustIssue(t, svc, user))
	if err != nil {
		t.Fatalf("self-issued token did not verify: %v", err)
	}
	_ = tok

	got := false
	handler := RequireAuth(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = true
		claims := ClaimsFrom(r.Context())
		if claims == nil {
			t.Error("expected claims on context for JWT auth")
		} else if claims.Email != user.Email {
			t.Errorf("claims.Email = %q; want %q", claims.Email, user.Email)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+mustIssue(t, svc, user))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with valid JWT; got %d", w.Code)
	}
	if !got {
		t.Error("inner handler was not reached")
	}
}

// An expired token must be rejected. NewIssuer clamps TTL <= 0 to 24h
// (defensive default for production), so we use the smallest positive
// TTL the API will accept and sleep just long enough for exp to pass.
// 5 ms sleep is comfortably above any reasonable JWT clock-skew window.
func TestAuth_RejectsExpiredJWT(t *testing.T) {
	expiredIssuer, err := auth.NewIssuer("test-secret", time.Millisecond)
	if err != nil {
		t.Fatalf("NewIssuer failed: %v", err)
	}
	svc := auth.NewService(stubStore{}, expiredIssuer)
	tok, err := expiredIssuer.Issue(auth.User{ID: "u1", Email: "x@y", IsAdmin: false})
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	time.Sleep(5 * time.Millisecond)

	handler := RequireAuth(svc)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("inner handler must not be reached for expired JWT")
	}))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for expired token; got %d", w.Code)
	}
}

// A token signed with a different secret must not verify.
func TestAuth_RejectsTamperedJWT(t *testing.T) {
	server, user := newTestAuth(t, time.Hour)

	// Different signing secret — same lib, same algo, different key.
	otherIssuer, err := auth.NewIssuer("attacker-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer failed: %v", err)
	}
	tok, err := otherIssuer.Issue(user)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}

	handler := RequireAuth(server)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("inner handler must not be reached for foreign-signed JWT")
	}))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for foreign-signed token; got %d", w.Code)
	}
}

// /api/auth/login is the one exception: writes there must pass through
// RequireAuth so the login handler can issue a token. We assert with
// no credentials at all.
func TestAuth_LoginPathIsBypassed(t *testing.T) {
	svc, _ := newTestAuth(t, time.Hour)

	got := false
	handler := RequireAuth(svc)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		got = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 on bypassed /api/auth/login; got %d", w.Code)
	}
	if !got {
		t.Error("login handler must be reached without credentials")
	}
}

func mustIssue(t *testing.T, svc *auth.Service, user auth.User) string {
	t.Helper()
	// The Service does not expose Issue directly; reach through the
	// canonical path by registering then logging in is overkill for a
	// unit test, so we just construct a parallel Issuer with the same
	// secret. This keeps the test self-contained.
	issuer, err := auth.NewIssuer("test-secret-not-used-anywhere-else", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer failed: %v", err)
	}
	tok, err := issuer.Issue(user)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	_ = svc
	return tok
}
