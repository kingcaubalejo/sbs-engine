package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Issuer holds JWT signing config. Built once at server startup from
// env so we never touch os.Getenv on the request path.
type Issuer struct {
	secret []byte
	ttl    time.Duration
}

// Claims is the JWT payload we issue. We embed RegisteredClaims so the
// standard exp/iat/sub fields are validated by the library; the rest is
// just enough to authorize without an extra DB lookup.
type Claims struct {
	UserID  string `json:"uid"`
	Email   string `json:"email"`
	IsAdmin bool   `json:"is_admin"`
	jwt.RegisteredClaims
}

// NewIssuer fails fast if the secret is missing — issuing tokens
// signed with an empty key would silently weaken auth to nothing.
func NewIssuer(secret string, ttl time.Duration) (*Issuer, error) {
	if secret == "" {
		return nil, ErrSecretNotConfigured
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Issuer{secret: []byte(secret), ttl: ttl}, nil
}

func (i *Issuer) Issue(user User) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:  user.ID,
		Email:   user.Email,
		IsAdmin: user.IsAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.ttl)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(i.secret)
}

func (i *Issuer) Verify(tokenStr string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		// Reject any algorithm other than the one we issued with.
		// Without this check, a "none" or RS256-confused token
		// could bypass signature validation.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return i.secret, nil
	})
	if err != nil || !parsed.Valid {
		return nil, ErrInvalidToken
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
