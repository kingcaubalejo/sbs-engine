package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sbs-engine/internal/auth"
	"sbs-engine/internal/middleware"
	"sbs-engine/internal/response"
)

// fakeUserStore is an in-memory UserStore for handler tests. It is
// deliberately tiny — we only need enough behaviour to exercise the
// login path: hit, miss, and infrastructure error.
type fakeUserStore struct {
	users  map[string]auth.User
	getErr error
}

func (f *fakeUserStore) GetUserByEmail(email string) (auth.User, bool, error) {
	if f.getErr != nil {
		return auth.User{}, false, f.getErr
	}
	u, ok := f.users[email]
	return u, ok, nil
}
func (f *fakeUserStore) CreateUser(u auth.User) (auth.User, error) {
	if f.users == nil {
		f.users = map[string]auth.User{}
	}
	f.users[u.Email] = u
	return u, nil
}
func (f *fakeUserStore) CountAdmins() (int64, error) { return 0, nil }

func newAuthTestServer(t *testing.T, store auth.UserStore) *Server {
	t.Helper()
	issuer, err := auth.NewIssuer("test-secret", time.Hour)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	return &Server{auth: auth.NewService(store, issuer)}
}

func postLogin(t *testing.T, s *Server, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.loginHandler(w, req)
	return w
}

// 503 when JWT_SECRET (and therefore *auth.Service) is unconfigured.
// The Server is constructed with auth=nil to model that environment.
func TestLoginHandler_AuthNotConfigured(t *testing.T) {
	s := &Server{}
	w := postLogin(t, s, map[string]string{"email": "a@b.com", "password": "x"})
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503; got %d", w.Code)
	}
}

func TestLoginHandler_InvalidJSON(t *testing.T) {
	s := newAuthTestServer(t, &fakeUserStore{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader("{not json"))
	w := httptest.NewRecorder()
	s.loginHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed JSON; got %d", w.Code)
	}
}

func TestLoginHandler_MissingFields(t *testing.T) {
	s := newAuthTestServer(t, &fakeUserStore{})

	cases := []map[string]string{
		{"email": "", "password": "x"},
		{"email": "a@b.com", "password": ""},
		{"email": "  ", "password": "x"},
		{},
	}
	for i, body := range cases {
		w := postLogin(t, s, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("case %d: expected 400; got %d", i, w.Code)
		}
	}
}

// Wrong password and unknown email must both produce the same 401 with
// the same message — login responses must not reveal which addresses
// are registered.
func TestLoginHandler_InvalidCredentialsCollapsesIdentically(t *testing.T) {
	hash, err := auth.HashPassword("correct-horse")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	store := &fakeUserStore{users: map[string]auth.User{
		"real@example.com": {Email: "real@example.com", PasswordHash: hash, IsAdmin: true},
	}}
	s := newAuthTestServer(t, store)

	wrongPassword := postLogin(t, s, map[string]string{"email": "real@example.com", "password": "battery-staple"})
	unknownUser := postLogin(t, s, map[string]string{"email": "nobody@example.com", "password": "anything"})

	for label, w := range map[string]*httptest.ResponseRecorder{"wrongPassword": wrongPassword, "unknownUser": unknownUser} {
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected 401; got %d", label, w.Code)
		}
	}
	if decodeErr(t, wrongPassword.Body.Bytes()) != decodeErr(t, unknownUser.Body.Bytes()) {
		t.Error("error messages must be identical to avoid leaking account existence")
	}
}

func TestLoginHandler_Success(t *testing.T) {
	hash, err := auth.HashPassword("correct-horse")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	store := &fakeUserStore{users: map[string]auth.User{
		"admin@example.com": {ID: "u1", Email: "admin@example.com", PasswordHash: hash, IsAdmin: true},
	}}
	s := newAuthTestServer(t, store)

	w := postLogin(t, s, map[string]string{"email": "admin@example.com", "password": "correct-horse"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200; got %d body=%s", w.Code, w.Body.String())
	}

	var env response.APIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !env.Success {
		t.Error("expected success=true")
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env.Data)
	}
	tok, _ := data["token"].(string)
	if tok == "" {
		t.Error("expected non-empty token in response")
	}
	user, _ := data["user"].(map[string]any)
	if user["email"] != "admin@example.com" {
		t.Errorf("user.email = %v; want admin@example.com", user["email"])
	}
	// PasswordHash is json:"-" — verify it never made it onto the wire.
	if _, leaked := user["password_hash"]; leaked {
		t.Error("password_hash leaked into login response")
	}
}

// Email is normalized to lowercase + trimmed before lookup. The fake
// store is keyed by the normalized form, so a mixed-case input must
// still find the user.
func TestLoginHandler_EmailNormalized(t *testing.T) {
	hash, _ := auth.HashPassword("p@ssword1")
	store := &fakeUserStore{users: map[string]auth.User{
		"user@example.com": {Email: "user@example.com", PasswordHash: hash},
	}}
	s := newAuthTestServer(t, store)

	w := postLogin(t, s, map[string]string{"email": "  USER@Example.com  ", "password": "p@ssword1"})
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 after email normalization; got %d", w.Code)
	}
}

// An infrastructure failure from the store (e.g. Mongo unreachable)
// must surface as 500, not 401. Distinguishing these matters for ops
// alerting — flooding 401s during a database outage hides the real
// signal.
func TestLoginHandler_StoreError(t *testing.T) {
	store := &fakeUserStore{getErr: errors.New("mongo: connection refused")}
	s := newAuthTestServer(t, store)

	w := postLogin(t, s, map[string]string{"email": "a@b.com", "password": "x"})
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on store failure; got %d", w.Code)
	}
}

func decodeErr(t *testing.T, body []byte) string {
	t.Helper()
	var env response.APIResponse
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error == nil {
		return ""
	}
	if s, ok := env.Error.(string); ok {
		return s
	}
	return ""
}

// ---------------------------------------------------------------------------
// registerHandler
// ---------------------------------------------------------------------------

// registerStore wraps fakeUserStore with a CreateUser hook so we can
// simulate auth.ErrDuplicateEmail without needing the real Mongo
// duplicate-key error path.
type registerStore struct {
	fakeUserStore
	createErr error
}

func (r *registerStore) CreateUser(u auth.User) (auth.User, error) {
	if r.createErr != nil {
		return auth.User{}, r.createErr
	}
	return r.fakeUserStore.CreateUser(u)
}

// postRegister sends a POST to /api/auth/register with the given body
// and a request context preloaded with the supplied claims (or no
// claims if claims is nil — used to test the safety default).
func postRegister(t *testing.T, s *Server, body any, claims *auth.Claims) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", &buf)
	req.Header.Set("Content-Type", "application/json")
	if claims != nil {
		req = req.WithContext(middleware.WithClaims(req.Context(), claims))
	}
	w := httptest.NewRecorder()
	s.registerHandler(w, req)
	return w
}

var (
	adminClaims = &auth.Claims{UserID: "admin-1", Email: "a@b.com", IsAdmin: true}
	plainClaims = &auth.Claims{UserID: "user-1", Email: "u@b.com", IsAdmin: false}
)

func TestRegister_AuthNotConfigured(t *testing.T) {
	s := &Server{}
	w := postRegister(t, s, map[string]any{"email": "x@y.com", "password": "longenough"}, adminClaims)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503; got %d", w.Code)
	}
}

// Defensive default: if the handler is reachable without claims (i.e.
// not gated by RequireAuth), it must refuse rather than treat
// missing-identity as machine-admin. This guards against a future
// routing mistake.
func TestRegister_RejectsMissingClaims(t *testing.T) {
	s := newAuthTestServer(t, &registerStore{})
	w := postRegister(t, s, map[string]any{"email": "x@y.com", "password": "longenough"}, nil)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 when no claims on context; got %d", w.Code)
	}
}

func TestRegister_RejectsNonAdminUser(t *testing.T) {
	s := newAuthTestServer(t, &registerStore{})
	w := postRegister(t, s, map[string]any{"email": "x@y.com", "password": "longenough"}, plainClaims)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-admin caller; got %d", w.Code)
	}
}

func TestRegister_AcceptsAdminUser(t *testing.T) {
	s := newAuthTestServer(t, &registerStore{fakeUserStore: fakeUserStore{users: map[string]auth.User{}}})
	w := postRegister(t, s, map[string]any{"email": "new@example.com", "password": "longenough"}, adminClaims)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for admin caller; got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRegister_InvalidJSON(t *testing.T) {
	s := newAuthTestServer(t, &registerStore{})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader("{not json"))
	req = req.WithContext(middleware.WithClaims(req.Context(), adminClaims))
	w := httptest.NewRecorder()
	s.registerHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed JSON; got %d", w.Code)
	}
}

func TestRegister_MissingFields(t *testing.T) {
	s := newAuthTestServer(t, &registerStore{})
	cases := []map[string]any{
		{"email": "", "password": "longenough"},
		{"email": "ok@example.com", "password": ""},
		{},
	}
	for i, body := range cases {
		w := postRegister(t, s, body, adminClaims)
		if w.Code != http.StatusBadRequest {
			t.Errorf("case %d: expected 400; got %d", i, w.Code)
		}
	}
}

func TestRegister_InvalidEmail(t *testing.T) {
	s := newAuthTestServer(t, &registerStore{})
	w := postRegister(t, s, map[string]any{"email": "not-an-email", "password": "longenough"}, adminClaims)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed email; got %d", w.Code)
	}
}

func TestRegister_WeakPassword(t *testing.T) {
	s := newAuthTestServer(t, &registerStore{fakeUserStore: fakeUserStore{users: map[string]auth.User{}}})
	w := postRegister(t, s, map[string]any{"email": "ok@example.com", "password": "short"}, adminClaims)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short password; got %d", w.Code)
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	store := &registerStore{
		fakeUserStore: fakeUserStore{users: map[string]auth.User{}},
		createErr:     auth.ErrDuplicateEmail,
	}
	s := newAuthTestServer(t, store)
	w := postRegister(t, s, map[string]any{"email": "exists@example.com", "password": "longenough"}, adminClaims)
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for duplicate email; got %d", w.Code)
	}
}

func TestRegister_SuccessReturnsUserWithoutPasswordHash(t *testing.T) {
	store := &registerStore{fakeUserStore: fakeUserStore{users: map[string]auth.User{}}}
	s := newAuthTestServer(t,store)

	w := postRegister(t, s, map[string]any{
		"email":    "new@example.com",
		"password": "longenough",
		"is_admin": true,
	}, adminClaims)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201; got %d body=%s", w.Code, w.Body.String())
	}
	var env response.APIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	user, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected user object, got %T", env.Data)
	}
	if user["email"] != "new@example.com" {
		t.Errorf("user.email = %v; want new@example.com", user["email"])
	}
	if user["is_admin"] != true {
		t.Errorf("user.is_admin = %v; want true (admin propagated from request)", user["is_admin"])
	}
	if _, leaked := user["password_hash"]; leaked {
		t.Error("password_hash leaked into register response")
	}

	// Confirm the user actually landed in the store with a real hash.
	stored, ok := store.users["new@example.com"]
	if !ok {
		t.Fatalf("user was not persisted to store")
	}
	if stored.PasswordHash == "" || stored.PasswordHash == "longenough" {
		t.Error("stored password_hash must be a bcrypt hash, not plaintext")
	}
}

