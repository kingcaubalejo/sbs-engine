package auth

import "golang.org/x/crypto/bcrypt"

// bcryptCost is intentionally above the library default (10). 12 keeps
// per-login latency under ~250ms on the EC2 target while raising the
// cost of an offline crack of leaked hashes by ~4x.
const bcryptCost = 12

// minPasswordLen is enforced at registration only. Existing hashes that
// predate this floor are still verifiable.
const minPasswordLen = 8

func HashPassword(plaintext string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func VerifyPassword(hash, plaintext string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}

func ValidatePasswordStrength(plaintext string) error {
	if len(plaintext) < minPasswordLen {
		return ErrWeakPassword
	}
	return nil
}
