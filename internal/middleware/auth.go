package middleware

import (
	"context"
	"net/http"
	"strings"

	"sbs-engine/internal/auth"
	"sbs-engine/internal/response"
)

// Auth context plumbing. RequireAuth attaches the verified JWT claims
// to the request context so downstream handlers can identify the
// caller and check IsAdmin without re-parsing the token.
type authCtxKey int

const claimsKey authCtxKey = iota

// RequireAuth gates non-GET requests on a JWT issued by /auth/login.
// GETs, HEADs, and CORS preflight pass through untouched. The login
// endpoint itself is also bypassed so it can mint tokens without
// already being authenticated.
//
// The token is read from the Authorization header in the form
// "Bearer <jwt>" and verified against the issuer's HS256 secret. On
// success the claims are attached to the request context (see
// ClaimsFrom) and the inner handler runs. On failure: 401 with a
// WWW-Authenticate hint.
//
// If the verifier is nil (i.e. JWT_SECRET was not configured at
// startup) every write returns 503 — fail-closed so a misconfigured
// deploy cannot silently accept anonymous mutations.
func RequireAuth(verifier *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}

			// Public mutation routes (login, future password-reset)
			// must be reachable without already being authenticated.
			// The list is small and explicit on purpose; do not turn
			// it into a prefix match without thinking about whether
			// child paths should also be public.
			if r.URL.Path == "/api/auth/login" {
				next.ServeHTTP(w, r)
				return
			}

			if verifier == nil {
				response.Error(w, http.StatusServiceUnavailable, "auth is not configured")
				return
			}

			bearer := extractBearer(r)
			if bearer == "" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="sbs-engine"`)
				response.Error(w, http.StatusUnauthorized, "missing token")
				return
			}

			claims, err := verifier.Verify(bearer)
			if err != nil {
				response.Error(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFrom returns the JWT claims attached to the request context.
// After RequireAuth has accepted a write, this is guaranteed non-nil
// for handlers in that chain. Returns nil when called from a handler
// that's reachable without authentication (GET, /auth/login).
func ClaimsFrom(ctx context.Context) *auth.Claims {
	if v, ok := ctx.Value(claimsKey).(*auth.Claims); ok {
		return v
	}
	return nil
}

// WithClaims returns a derived context with the supplied claims
// attached under the same key RequireAuth uses. Tests inject identity
// with this; in production, only RequireAuth should call it.
func WithClaims(ctx context.Context, claims *auth.Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

func extractBearer(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) <= len(prefix) {
		return ""
	}
	if !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}
