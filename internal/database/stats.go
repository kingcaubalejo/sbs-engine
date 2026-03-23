package database

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// Stats holds platform-level aggregate counts.
type Stats struct {
	TotalVolumes   int64 `json:"total_volumes"`
	TotalSermons   int64 `json:"total_sermons"`
	TotalLanguages int   `json:"total_languages"`
}

func (d *Database) GetStats() Stats {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	volumeCount, err := d.DB.Collection("volumes").CountDocuments(ctx, bson.M{})
	if err != nil {
		panic(err)
	}

	sermonCount, err := d.DB.Collection("books").CountDocuments(ctx, bson.M{})
	if err != nil {
		panic(err)
	}

	return Stats{
		TotalVolumes:   volumeCount,
		TotalSermons:   sermonCount,
		TotalLanguages: len(supportedLanguages),
	}
}
