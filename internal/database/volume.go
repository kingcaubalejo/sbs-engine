package database

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Volume struct {
	ID            int    `json:"id" bson:"id"`
	VolumeNumber  int    `json:"volume_number" bson:"volume_number"`
	ImageURL      string `json:"image_url" bson:"image_url"`
	TotalSBS      int    `json:"total_sbs" bson:"total_sbs"`
	TotalLanguages int   `json:"total_languages" bson:"total_languages"`
}

// volumesListLimit caps GetVolumes — small enough that an unbounded
// scan cannot blow up egress on a public endpoint, large enough that
// any realistic library fits in one response. Use GetVolumesPaginated
// for collections that may exceed this.
const volumesListLimit = 200

func (d *Database) GetVolumes() []Volume {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	collection := d.DB.Collection("volumes")

	cursor, err := collection.Find(ctx, bson.M{}, options.Find().SetLimit(volumesListLimit).SetSort(bson.D{{Key: "volume_number", Value: 1}}))
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

// GetVolumeByID retrieves a single volume by its integer ID.
func (d *Database) GetVolumeByID(id int) (Volume, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	var volume Volume
	err := d.DB.Collection("volumes").FindOne(ctx, bson.M{"id": id}).Decode(&volume)
	if err == mongo.ErrNoDocuments {
		return Volume{}, false
	}
	if err != nil {
		panic(err)
	}
	return volume, true
}

// PaginatedVolumes wraps a page of volumes with total-count metadata.
type PaginatedVolumes struct {
	Items []Volume `json:"items"`
	Total int64    `json:"total"`
	Page  int      `json:"page"`
	Limit int      `json:"limit"`
	Pages int64    `json:"pages"`
}

// GetVolumesPaginated returns a page of volumes and the total document count.
func (d *Database) GetVolumesPaginated(page, limit int) ([]Volume, int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	collection := d.DB.Collection("volumes")

	total, err := collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		panic(err)
	}

	skip := int64((page - 1) * limit)
	opts := options.Find().SetSkip(skip).SetLimit(int64(limit)).SetSort(bson.D{{Key: "volume_number", Value: 1}})

	cursor, err := collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		panic(err)
	}
	defer cursor.Close(ctx)

	var volumes []Volume
	if err := cursor.All(ctx, &volumes); err != nil {
		panic(err)
	}
	return volumes, total
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