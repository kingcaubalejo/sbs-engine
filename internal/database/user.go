package database

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"sbs-engine/internal/auth"
)

// userDoc is the on-disk shape. It mirrors auth.User but uses
// primitive.ObjectID for _id so Mongo can autogenerate it; we expose
// the hex string back to the caller through auth.User.ID.
type userDoc struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	Email        string             `bson:"email"`
	PasswordHash string             `bson:"password_hash"`
	IsAdmin      bool               `bson:"is_admin"`
	CreatedAt    time.Time          `bson:"created_at"`
}

func (u userDoc) toAuthUser() auth.User {
	return auth.User{
		ID:           u.ID.Hex(),
		Email:        u.Email,
		PasswordHash: u.PasswordHash,
		IsAdmin:      u.IsAdmin,
		CreatedAt:    u.CreatedAt,
	}
}

func (d *Database) GetUserByEmail(email string) (auth.User, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var doc userDoc
	err := d.DB.Collection("users").FindOne(ctx, bson.M{"email": email}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return auth.User{}, false, nil
	}
	if err != nil {
		return auth.User{}, false, err
	}
	return doc.toAuthUser(), true, nil
}

func (d *Database) CreateUser(user auth.User) (auth.User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	doc := userDoc{
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
		IsAdmin:      user.IsAdmin,
		CreatedAt:    user.CreatedAt,
	}

	res, err := d.DB.Collection("users").InsertOne(ctx, doc)
	if err != nil {
		// Translate Mongo's duplicate-key error (E11000) to the
		// auth-package sentinel so the HTTP layer can map it to 409
		// without importing the Mongo driver.
		if mongo.IsDuplicateKeyError(err) {
			return auth.User{}, auth.ErrDuplicateEmail
		}
		return auth.User{}, err
	}
	if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
		doc.ID = oid
	}
	return doc.toAuthUser(), nil
}

func (d *Database) CountAdmins() (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return d.DB.Collection("users").CountDocuments(ctx, bson.M{"is_admin": true})
}
