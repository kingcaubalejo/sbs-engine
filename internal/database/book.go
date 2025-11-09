package database

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	_"go.mongodb.org/mongo-driver/mongo"
)

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

	languages := map[string]int{
		"en": 1,
		"de": 2,
	}
	
	collection := d.DB.Collection("books")
	filter := bson.M{
		"volume_number": volumeId, 
		"id": languages[lang],
	}

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		panic(err)
	}

	var results []Sermon
	if err = cursor.All(ctx, &results); err != nil {
		panic(err)
	}
	return results
}