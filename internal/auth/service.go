package auth

import (
	"strings"
	"time"
)

// Service is the auth orchestrator the HTTP layer talks to. It owns
// the UserStore and Issuer so handlers never reach into either
// directly — keeps business rules (e.g. "always normalize email",
// "constant-time-ish failure") in one place.
type Service struct {
	store  UserStore
	issuer *Issuer
}

func NewService(store UserStore, issuer *Issuer) *Service {
	return &Service{store: store, issuer: issuer}
}

// Login returns a signed JWT or ErrInvalidCredentials. We deliberately
// collapse "user not found" and "wrong password" into the same error
// so the response does not leak account existence.
func (s *Service) Login(email, password string) (string, User, error) {
	email = normalizeEmail(email)

	user, found, err := s.store.GetUserByEmail(email)
	if err != nil {
		return "", User{}, err
	}
	if !found {
		// Run a dummy verify to keep timing roughly constant. bcrypt
		// dominates this code path, so skipping it on miss would be a
		// timing oracle for "is this email registered".
		_ = VerifyPassword("$2a$12$dummyhashtoresisttimingleak............................", password)
		return "", User{}, ErrInvalidCredentials
	}
	if !VerifyPassword(user.PasswordHash, password) {
		return "", User{}, ErrInvalidCredentials
	}

	token, err := s.issuer.Issue(user)
	if err != nil {
		return "", User{}, err
	}
	return token, user, nil
}

// CreateUser is used by the bootstrap path and (later) the register
// endpoint. It hashes the password and enforces minimum strength.
func (s *Service) CreateUser(email, password string, isAdmin bool) (User, error) {
	if err := ValidatePasswordStrength(password); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	user := User{
		Email:        normalizeEmail(email),
		PasswordHash: hash,
		IsAdmin:      isAdmin,
		CreatedAt:    time.Now().UTC(),
	}
	return s.store.CreateUser(user)
}

// AdminCount exposes the store's count for the bootstrap decision in
// the server layer ("seed an admin only if none exist").
func (s *Service) AdminCount() (int64, error) {
	return s.store.CountAdmins()
}

func (s *Service) Verify(token string) (*Claims, error) {
	return s.issuer.Verify(token)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
