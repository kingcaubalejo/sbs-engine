package database

import (
	"context"
	"fmt"
	"log"
	"os"

	_ "github.com/joho/godotenv/autoload"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Service interface {
	GetVolumes() []Volume
	GetBooksByVolume(volumeId int, lang string) []Sermon
	HealthCheck() map[string]string
	GetDonation() Donation
}

type Database struct {
	DB *mongo.Database
	Client *mongo.Client
}

var (
	username = os.Getenv("BLUEPRINT_DB_USERNAME")
	password = os.Getenv("BLUEPRINT_DB_ROOT_PASSWORD")
	host     = os.Getenv("BLUEPRINT_DB_HOST")
	port     = os.Getenv("BLUEPRINT_DB_PORT")
	database = os.Getenv("BLUEPRINT_DB_NAME")
)

func NewDatabase() *Database {
	var uri string
	if username == "" && password == "" {
		uri = fmt.Sprintf("mongodb://%s:%s/%s", host, port, database)
	} else {
		uri = fmt.Sprintf("mongodb://%s:%s@%s:%s/%s?ssl=false&authSource=admin",
			username, password, host, port, database,
		)
	}
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal(err)
	}

	return &Database{
		DB:     client.Database(database),
		Client: client,
	}
}

func (d *Database) HealthCheck() map[string]string {
	return map[string]string{
		"status":  "up",
		"message": "Database is healthy",
	}
}