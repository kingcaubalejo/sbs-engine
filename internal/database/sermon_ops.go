package database

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// GetSermonByLocation retrieves a single sermon by volume number, SBS number, and language.
func (d *Database) GetSermonByLocation(volumeNumber, sbsNumber int, lang string) (Sermon, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	langID, ok := supportedLanguages[lang]
	if !ok {
		langID = supportedLanguages["en"]
	}

	collection := d.DB.Collection("books")
	filter := bson.M{
		"volume_number": volumeNumber,
		"sbs_number":    sbsNumber,
		"id":            langID,
	}

	var sermon Sermon
	err := collection.FindOne(ctx, filter).Decode(&sermon)
	if err == mongo.ErrNoDocuments {
		return Sermon{}, false
	}
	if err != nil {
		panic(err)
	}
	return sermon, true
}

// SearchSermons performs a case-insensitive search over title and content fields.
func (d *Database) SearchSermons(query, lang string) []Sermon {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	langID, ok := supportedLanguages[lang]
	if !ok {
		langID = supportedLanguages["en"]
	}

	collection := d.DB.Collection("books")
	filter := bson.M{
		"id": langID,
		"$or": bson.A{
			bson.M{"title": bson.M{"$regex": query, "$options": "i"}},
			bson.M{"content": bson.M{"$regex": query, "$options": "i"}},
		},
	}

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		panic(err)
	}
	defer cursor.Close(ctx)

	var sermons []Sermon
	if err := cursor.All(ctx, &sermons); err != nil {
		panic(err)
	}
	return sermons
}

// GetRandomSermon returns a single random sermon for the given language using MongoDB $sample.
func (d *Database) GetRandomSermon(lang string) (Sermon, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	langID, ok := supportedLanguages[lang]
	if !ok {
		langID = supportedLanguages["en"]
	}

	collection := d.DB.Collection("books")
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"id": langID}}},
		{{Key: "$sample", Value: bson.M{"size": 1}}},
	}

	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		panic(err)
	}
	defer cursor.Close(ctx)

	var sermons []Sermon
	if err := cursor.All(ctx, &sermons); err != nil {
		panic(err)
	}
	if len(sermons) == 0 {
		return Sermon{}, false
	}
	return sermons[0], true
}

// CreateSermon inserts a new sermon into the books collection.
func (d *Database) CreateSermon(sermon Sermon) Sermon {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	sermon.ObjectID = primitive.NewObjectID()
	collection := d.DB.Collection("books")
	_, err := collection.InsertOne(ctx, sermon)
	if err != nil {
		panic(err)
	}
	return sermon
}

// DeleteSermon removes a sermon by its MongoDB ObjectID hex string.
func (d *Database) DeleteSermon(objectID string) bool {
	oid, err := primitive.ObjectIDFromHex(objectID)
	if err != nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	collection := d.DB.Collection("books")
	result, err := collection.DeleteOne(ctx, bson.M{"_id": oid})
	if err != nil {
		panic(err)
	}
	return result.DeletedCount > 0
}

// PatchSermon partially updates a sermon by its MongoDB ObjectID hex string.
// Returns the updated sermon and true, or zero-value and false if not found or invalid ID.
func (d *Database) PatchSermon(objectID string, updates map[string]interface{}) (Sermon, bool) {
	oid, err := primitive.ObjectIDFromHex(objectID)
	if err != nil {
		return Sermon{}, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	collection := d.DB.Collection("books")
	filter := bson.M{"_id": oid}

	result, err := collection.UpdateOne(ctx, filter, bson.M{"$set": updates})
	if err != nil {
		panic(err)
	}
	if result.MatchedCount == 0 {
		return Sermon{}, false
	}

	var sermon Sermon
	if err := collection.FindOne(ctx, filter).Decode(&sermon); err != nil {
		panic(err)
	}
	return sermon, true
}
