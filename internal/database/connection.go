package database

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Service interface {
	// Volume operations
	GetVolumes() []Volume
	GetVolumeByID(id int) (Volume, bool)
	GetVolumesPaginated(page, limit int) ([]Volume, int64)
	CreateVolume(volume Volume) Volume
	UpdateVolume(id int, volume Volume) Volume
	PatchVolume(id int, updates map[string]interface{}) Volume
	DeleteVolume(id int) bool

	// Sermon operations
	GetBooksByVolume(volumeId int, lang string) []Sermon
	GetSermonByLocation(volumeNumber, sbsNumber int, lang string) (Sermon, bool)
	SearchSermons(query, lang string) []Sermon
	GetRandomSermon(lang string) (Sermon, bool)
	CreateSermon(sermon Sermon) Sermon
	DeleteSermon(objectID string) bool
	PatchSermon(objectID string, updates map[string]interface{}) (Sermon, bool)

	// Utility
	HealthCheck() map[string]string
	GetDonation() Donation
	GetStats() Stats
	GetLanguages() []Language
}

type Database struct {
	DB     *mongo.Database
	Client *mongo.Client
}

// buildURI assembles the MongoDB connection string from env vars. When
// MONGO_USE_SRV=true the mongodb+srv:// scheme is used (required by
// MongoDB Atlas), otherwise the plain mongodb:// host:port form is used
// for self-hosted or local Docker instances.
func buildURI() string {
	username := os.Getenv("BLUEPRINT_DB_USERNAME")
	password := os.Getenv("BLUEPRINT_DB_ROOT_PASSWORD")
	host := os.Getenv("BLUEPRINT_DB_HOST")
	port := os.Getenv("BLUEPRINT_DB_PORT")
	dbName := os.Getenv("BLUEPRINT_DB_NAME")

	if os.Getenv("MONGO_USE_SRV") == "true" {
		return fmt.Sprintf("mongodb+srv://%s:%s@%s/%s?retryWrites=true&w=majority",
			username, password, host, dbName)
	}
	if username == "" && password == "" {
		return fmt.Sprintf("mongodb://%s:%s/%s", host, port, dbName)
	}
	return fmt.Sprintf("mongodb://%s:%s@%s:%s/%s?authSource=admin",
		username, password, host, port, dbName)
}

func NewDatabase() *Database {
	uri := buildURI()
	dbName := os.Getenv("BLUEPRINT_DB_NAME")

	clientOpts := options.Client().
		ApplyURI(uri).
		SetMaxPoolSize(50).
		SetMinPoolSize(5).
		SetServerSelectionTimeout(5 * time.Second).
		SetConnectTimeout(10 * time.Second).
		SetSocketTimeout(30 * time.Second).
		SetRetryWrites(true).
		SetRetryReads(true)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		slog.Error("mongo connect failed", "error", err)
		os.Exit(1)
	}

	d := &Database{
		DB:     client.Database(dbName),
		Client: client,
	}

	// Best-effort index creation. Do not fail startup if it errors —
	// indexes may already exist from a prior run, or the user may not
	// have createIndex privileges in production. The application can
	// still function (slowly) without them.
	if err := d.ensureIndexes(); err != nil {
		slog.Warn("index creation failed (continuing)", "error", err)
	}

	return d
}

func (d *Database) HealthCheck() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := d.Client.Ping(ctx, nil); err != nil {
		return map[string]string{
			"status":  "down",
			"message": "Database is unreachable",
		}
	}
	return map[string]string{
		"status":  "up",
		"message": "Database is healthy",
	}
}
