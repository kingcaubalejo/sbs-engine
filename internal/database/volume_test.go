package database

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestGetVolumes(t *testing.T) {
	db := NewDatabase()
	defer db.Client.Disconnect(context.Background())

	collection := db.DB.Collection("volumes")
	testVolume := bson.M{
		"id":              1,
		"volume_number":   1,
		"image_url":       "test.jpg",
		"total_sbs":       10,
		"total_languages": 2,
	}
	
	_, err := collection.InsertOne(context.Background(), testVolume)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	results := db.GetVolumes()
	
	if len(results) == 0 {
		t.Fatal("Expected at least one volume")
	}

	if results[0].VolumeNumber != 1 {
		t.Errorf("Expected volume number 1, got %d", results[0].VolumeNumber)
	}

	collection.DeleteMany(context.Background(), bson.M{})
}