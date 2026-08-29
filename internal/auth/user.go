// Package auth provides password-based login and JWT issue/verify for
// sbs-engine. It is intentionally self-contained: storage is abstracted
// behind UserStore so the package has no MongoDB dependency, and HTTP
// concerns live in the server layer. If we later split auth into its
// own service, this package lifts cleanly into a new module.
package auth

import "time"

// User is the canonical authenticated identity. PasswordHash is a
// bcrypt-encoded string; the plaintext password never lives on this
// struct or in storage.
type User struct {
	ID           string    `json:"id" bson:"_id,omitempty"`
	Email        string    `json:"email" bson:"email"`
	PasswordHash string    `json:"-" bson:"password_hash"`
	IsAdmin      bool      `json:"is_admin" bson:"is_admin"`
	CreatedAt    time.Time `json:"created_at" bson:"created_at"`
}

// UserStore is the storage contract auth requires. Implementations live
// outside this package (see internal/database/user.go for the Mongo
// implementation). Methods return (User{}, false, nil) for "not found"
// and reserve error for actual infrastructure failures.
type UserStore interface {
	GetUserByEmail(email string) (User, bool, error)
	CreateUser(user User) (User, error)
	CountAdmins() (int64, error)
}
