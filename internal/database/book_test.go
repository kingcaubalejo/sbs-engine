package database

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestGetBooksByVolume(t *testing.T) {
	db := NewDatabase()
	defer db.Client.Disconnect(context.Background())

	collection := db.DB.Collection("books")
	testSermon := bson.M{
		"_id":           primitive.NewObjectID(),
		"id":            1,
		"title":         "Test Sermon",
		"quote":         "Test Quote",
		"sbs_number":    1,
		"volume_number": 1,
		"book_number":   1,
		"image_url":     "test.jpg",
		"content":       "Test content",
	}
	
	_, err := collection.InsertOne(context.Background(), testSermon)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	results := db.GetBooksByVolume(1, "en")
	
	if len(results) == 0 {
		t.Fatal("Expected at least one result")
	}

	collection.DeleteMany(context.Background(), bson.M{})
}