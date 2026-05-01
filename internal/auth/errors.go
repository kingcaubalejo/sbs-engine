package auth

import "errors"

// Sentinel errors so callers (and tests) can switch on cause without
// pattern-matching strings. Keep these stable — they cross the package
// boundary into HTTP handler error mapping.
var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrWeakPassword       = errors.New("password does not meet strength requirements")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrSecretNotConfigured = errors.New("JWT_SECRET is not configured")
	// ErrDuplicateEmail is returned by the UserStore when an email
	// already exists. The store layer translates infrastructure errors
	// (e.g. Mongo duplicate-key) into this sentinel so HTTP handlers
	// can decide on a status code without pattern-matching driver errors.
	ErrDuplicateEmail = errors.New("email already exists")
)
