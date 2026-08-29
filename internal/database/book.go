package database

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// booksListLimit caps GetBooksByVolume so that a single volume cannot
// stream an unbounded number of sermons over the wire. Volumes
// typically hold a few dozen sermons; 200 is a comfortable headroom.
const booksListLimit = 200

type Sermon struct {
    ObjectID     primitive.ObjectID `bson:"_id" json:"_id"`
    ID           int                `bson:"id" json:"id"`
    Title        string             `bson:"title" json:"title"`
    Quote        string             `bson:"quote" json:"quote"`
    SbsNumber    int                `bson:"sbs_number" json:"sbs_number"`
    VolumeNumber int                `bson:"volume_number" json:"volume_number"`
    BookNumber   int                `bson:"book_number" json:"book_number"`
    ImageURL     string             `bson:"image_url" json:"image_url"`
    Content      string             `bson:"content" json:"content"`
}

func (d *Database) GetBooksByVolume(volumeId int, lang string) []Sermon {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	collection := d.DB.Collection("books")
	filter := bson.M{
		"volume_number": volumeId,
		"id":            supportedLanguages[lang],
	}

	cursor, err := collection.Find(ctx, filter, options.Find().SetLimit(booksListLimit).SetSort(bson.D{{Key: "sbs_number", Value: 1}}))
	if err != nil {
		panic(err)
	}

	var results []Sermon
	if err = cursor.All(ctx, &results); err != nil {
		panic(err)
	}
	return results
}