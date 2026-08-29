package database

import (
	"context"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// searchResultLimit caps how many documents SearchSermons returns. This
// is a safety bound for both bandwidth (responses cannot grow without
// limit) and Mongo work. Combined with a server-side regex fallback
// length cap this keeps a single search from being able to scan and
// stream the entire collection.
const searchResultLimit = 50

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

// SearchSermons uses the books_text index for full-text search over title
// and content. The content field is excluded from the projection so list
// responses stay small; clients should call GetSermonByLocation to fetch
// the full body for a result they want to read. If the text index is
// missing (e.g. on a fresh database where ensureIndexes failed) the
// query returns no rows rather than falling back to a COLLSCAN regex —
// regex over title+content is the very abuse vector this rewrite exists
// to close.
func (d *Database) SearchSermons(query, lang string) []Sermon {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	langID, ok := supportedLanguages[lang]
	if !ok {
		langID = supportedLanguages["en"]
	}

	collection := d.DB.Collection("books")

	filter := bson.M{
		"id":    langID,
		"$text": bson.M{"$search": sanitizeTextSearch(query)},
	}
	opts := options.Find().
		SetProjection(bson.M{
			"score":   bson.M{"$meta": "textScore"},
			"content": 0,
		}).
		SetSort(bson.M{"score": bson.M{"$meta": "textScore"}}).
		SetLimit(searchResultLimit)

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		// A missing $text index surfaces as a server-side error; treat
		// the search as empty rather than panicking and crashing the
		// server. The recover middleware would catch a panic but the
		// caller would see a 500 on every search until indexes are
		// rebuilt — an empty result set is friendlier.
		return []Sermon{}
	}
	defer cursor.Close(ctx)

	var sermons []Sermon
	if err := cursor.All(ctx, &sermons); err != nil {
		return []Sermon{}
	}
	return sermons
}

// sanitizeTextSearch makes the search input safe for $text. The text
// search syntax supports phrase matching (with double quotes), negation
// (with a leading -), and term separation (whitespace). For our public,
// unauthenticated API we strip those operators and reduce input to bare
// alphanumeric tokens, so a malicious user cannot craft an expensive
// negation-heavy query. Result is space-separated tokens.
func sanitizeTextSearch(q string) string {
	re := regexp.MustCompile(`[^\p{L}\p{N}\s]+`)
	cleaned := re.ReplaceAllString(q, " ")
	return cleaned
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
