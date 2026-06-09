package repo

import (
	"context"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

type MongoRepo struct {
	client *mongo.Client
	coll   *mongo.Collection
	log    *slog.Logger
}

func NewMongoRepo(ctx context.Context, uri, dbName string, log *slog.Logger) (*MongoRepo, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, err
	}
	wc := writeconcern.Majority()
	coll := client.Database(dbName).Collection("click_events", options.Collection().SetWriteConcern(wc))
	r := &MongoRepo{client: client, coll: coll, log: log}
	if err := r.ensureIndexes(ctx); err != nil {
		_ = client.Disconnect(ctx)
		return nil, err
	}
	return r, nil
}

func (r *MongoRepo) ensureIndexes(ctx context.Context) error {
	compound := mongo.IndexModel{
		Keys: bson.D{
			{Key: "code", Value: 1},
			{Key: "timestamp", Value: -1},
		},
	}
	ttlSeconds := int32(14 * 24 * 60 * 60)
	ttl := mongo.IndexModel{
		Keys:    bson.D{{Key: "received_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(ttlSeconds),
	}
	_, err := r.coll.Indexes().CreateMany(ctx, []mongo.IndexModel{compound, ttl})
	return err
}

func (r *MongoRepo) Insert(ctx context.Context, event ClickEvent) error {
	_, err := r.coll.InsertOne(ctx, bson.D{
		{Key: "code", Value: event.Code},
		{Key: "timestamp", Value: event.Timestamp},
		{Key: "referrer", Value: event.Referrer},
		{Key: "ip_hash", Value: event.IPHash},
		{Key: "received_at", Value: event.ReceivedAt},
	})
	return err
}

func (r *MongoRepo) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return r.client.Ping(pingCtx, nil)
}

func (r *MongoRepo) Close(ctx context.Context) error {
	return r.client.Disconnect(ctx)
}
