package database

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ensureIndexes creates the indexes required for fast lookups and the
// $text search rewrite. It is called once on startup; existing indexes
// are no-ops at the driver level. The text index uses higher weight on
// title than content so phrase matches in titles rank above
// matches in body content.
func (d *Database) ensureIndexes() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	books := d.DB.Collection("books")
	if _, err := books.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "volume_number", Value: 1},
				{Key: "sbs_number", Value: 1},
				{Key: "id", Value: 1},
			},
			Options: options.Index().SetName("volume_sbs_lang"),
		},
		{
			Keys: bson.D{
				{Key: "id", Value: 1},
				{Key: "volume_number", Value: 1},
			},
			Options: options.Index().SetName("lang_volume"),
		},
		{
			Keys: bson.D{
				{Key: "title", Value: "text"},
				{Key: "content", Value: "text"},
			},
			Options: options.Index().
				SetName("books_text").
				SetWeights(bson.M{"title": 5, "content": 1}).
				SetDefaultLanguage("english"),
		},
	}); err != nil {
		return err
	}

	volumes := d.DB.Collection("volumes")
	if _, err := volumes.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "id", Value: 1}},
			Options: options.Index().SetName("volume_id").SetUnique(true),
		},
	}); err != nil {
		return err
	}

	return nil
}
