package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"

	"sbs-engine/internal/auth"
	"sbs-engine/internal/middleware"
	"sbs-engine/internal/response"
)

// loginRequest is the body shape for POST /auth/login. Only email and
// password are accepted from the client — never IsAdmin or any other
// privilege bit, which would let a caller escalate their own account.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// loginResponse is what we hand back on success. The user document is
// the public auth.User shape (PasswordHash is json:"-" so it never
// goes over the wire).
type loginResponse struct {
	Token string    `json:"token"`
	User  auth.User `json:"user"`
}

// loginHandler godoc
//
//	@Summary		Exchange email + password for a JWT
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		loginRequest	true	"Credentials"
//	@Success		200		{object}	response.APIResponse
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		503		{object}	response.APIResponse
//	@Router			/auth/login [post]
func (s *Server) loginHandler(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		response.Error(w, http.StatusServiceUnavailable, "auth is not configured")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		response.Error(w, http.StatusBadRequest, "email and password are required")
		return
	}

	token, user, err := s.auth.Login(req.Email, req.Password)
	if err != nil {
		// Keep the message identical for "no such user" and "wrong
		// password" so we don't tell an attacker which emails are
		// registered. The auth.Service already does the constant-time
		// dummy bcrypt for missing users.
		if errors.Is(err, auth.ErrInvalidCredentials) {
			response.Error(w, http.StatusUnauthorized, "invalid email or password")
			return
		}
		response.Error(w, http.StatusInternalServerError, "login failed")
		return
	}

	response.Success(w, "login successful", loginResponse{Token: token, User: user})
}

// registerRequest is the body shape for POST /auth/register. The
// optional IsAdmin flag is intentionally accepted from the body — the
// route is itself admin-gated, so an admin promoting a colleague is the
// expected use case. A non-admin caller can never reach this handler
// because callerIsAdmin rejects them before we read the body.
type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	IsAdmin  bool   `json:"is_admin"`
}

// registerHandler godoc
//
//	@Summary		Create a new user (admin only)
//	@Description	Existing admin users can create additional accounts. The new user must log in via /auth/login to obtain a JWT — registration does not auto-issue a token.
//	@Tags			auth
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		registerRequest	true	"New user credentials"
//	@Success		201		{object}	response.APIResponse
//	@Failure		400		{object}	response.APIResponse
//	@Failure		401		{object}	response.APIResponse
//	@Failure		403		{object}	response.APIResponse
//	@Failure		409		{object}	response.APIResponse
//	@Failure		503		{object}	response.APIResponse
//	@Router			/auth/register [post]
func (s *Server) registerHandler(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		response.Error(w, http.StatusServiceUnavailable, "auth is not configured")
		return
	}
	if !callerIsAdmin(r) {
		response.Error(w, http.StatusForbidden, "admin only")
		return
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		response.Error(w, http.StatusBadRequest, "email and password are required")
		return
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		response.Error(w, http.StatusBadRequest, "email is not a valid address")
		return
	}

	user, err := s.auth.CreateUser(req.Email, req.Password, req.IsAdmin)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrWeakPassword):
			response.Error(w, http.StatusBadRequest, "password does not meet strength requirements")
		case errors.Is(err, auth.ErrDuplicateEmail):
			response.Error(w, http.StatusConflict, "email is already registered")
		default:
			response.Error(w, http.StatusInternalServerError, "could not create user")
		}
		return
	}

	response.JSON(w, http.StatusCreated, response.APIResponse{
		Success: true,
		Message: "user created",
		Data:    user,
	})
}

// callerIsAdmin returns true when the request reached the handler with
// a verified admin JWT on its context. RequireAuth must have already
// accepted the request, so a missing claims slot indicates a routing
// mistake (the route was not gated by RequireAuth) — we reject as a
// safety default.
func callerIsAdmin(r *http.Request) bool {
	claims := middleware.ClaimsFrom(r.Context())
	if claims == nil {
		return false
	}
	return claims.IsAdmin
}
