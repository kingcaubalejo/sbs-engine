package database

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	_"go.mongodb.org/mongo-driver/mongo"
)

type Volume struct {
	ID            int    `json:"id" bson:"id"`
	VolumeNumber  int    `json:"volume_number" bson:"volume_number"`
	ImageURL      string `json:"image_url" bson:"image_url"`
	TotalSBS      int    `json:"total_sbs" bson:"total_sbs"`
	TotalLanguages int   `json:"total_languages" bson:"total_languages"`
}

func (d *Database) GetVolumes() []Volume {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	collection := d.DB.Collection("volumes")
	
	cursor, err := collection.Find(ctx, bson.M{})
	if err != nil {
		panic(err)
	}
	defer cursor.Close(ctx)

	var volumes []Volume
	if err := cursor.All(ctx, &volumes); err != nil {
		panic(err)
	}
	return volumes
}