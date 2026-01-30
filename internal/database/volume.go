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

func (d *Database) CreateVolume(volume Volume) Volume {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	collection := d.DB.Collection("volumes")
	
	_, err := collection.InsertOne(ctx, volume)
	if err != nil {
		panic(err)
	}
	return volume
}

func (d *Database) UpdateVolume(id int, volume Volume) Volume {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	collection := d.DB.Collection("volumes")
	
	_, err := collection.ReplaceOne(ctx, bson.M{"id": id}, volume)
	if err != nil {
		panic(err)
	}
	return volume
}

func (d *Database) PatchVolume(id int, updates map[string]interface{}) Volume {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	collection := d.DB.Collection("volumes")
	
	_, err := collection.UpdateOne(ctx, bson.M{"id": id}, bson.M{"$set": updates})
	if err != nil {
		panic(err)
	}

	var volume Volume
	err = collection.FindOne(ctx, bson.M{"id": id}).Decode(&volume)
	if err != nil {
		panic(err)
	}
	return volume
}

func (d *Database) DeleteVolume(id int) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	collection := d.DB.Collection("volumes")
	
	result, err := collection.DeleteOne(ctx, bson.M{"id": id})
	if err != nil {
		panic(err)
	}
	return result.DeletedCount > 0
}