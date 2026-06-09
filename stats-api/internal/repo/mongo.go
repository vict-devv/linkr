package repo

import (
	"context"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type MongoStatsRepo struct {
	client *mongo.Client
	coll   *mongo.Collection
	log    *slog.Logger
}

func NewMongoStatsRepo(ctx context.Context, uri, dbName, collName string, log *slog.Logger) (*MongoStatsRepo, error) {
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
	coll := client.Database(dbName).Collection(collName)
	return &MongoStatsRepo{client: client, coll: coll, log: log}, nil
}

func (r *MongoStatsRepo) TotalClicks(ctx context.Context, code string) (int64, error) {
	start := time.Now()
	defer r.logSlow(ctx, start, code, "total_clicks")
	return r.coll.CountDocuments(ctx, bson.D{{Key: "code", Value: code}})
}

func (r *MongoStatsRepo) ClicksOverTime(ctx context.Context, code string, days int) ([]ClicksOverTime, error) {
	start := time.Now()
	defer r.logSlow(ctx, start, code, "clicks_over_time")

	since := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -(days - 1))
	pipeline := bson.A{
		bson.D{{Key: "$match", Value: bson.D{
			{Key: "code", Value: code},
			{Key: "timestamp", Value: bson.D{{Key: "$gte", Value: since}}},
		}}},
		bson.D{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{{Key: "$dateToString", Value: bson.D{
				{Key: "format", Value: "%Y-%m-%d"},
				{Key: "date", Value: "$timestamp"},
			}}}},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		bson.D{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
	}
	cursor, err := r.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var raw []struct {
		ID    string `bson:"_id"`
		Count int64  `bson:"count"`
	}
	if err := cursor.All(ctx, &raw); err != nil {
		return nil, err
	}

	out := make([]ClicksOverTime, len(raw))
	for i, v := range raw {
		out[i] = ClicksOverTime{Date: v.ID, Count: v.Count}
	}
	return out, nil
}

func (r *MongoStatsRepo) TopReferrers(ctx context.Context, code string, limit int) ([]TopReferrer, error) {
	start := time.Now()
	defer r.logSlow(ctx, start, code, "top_referrers")

	pipeline := bson.A{
		bson.D{{Key: "$match", Value: bson.D{{Key: "code", Value: code}}}},
		bson.D{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$referrer"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		bson.D{{Key: "$sort", Value: bson.D{{Key: "count", Value: -1}}}},
		bson.D{{Key: "$limit", Value: limit}},
	}
	cursor, err := r.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var raw []struct {
		ID    string `bson:"_id"`
		Count int64  `bson:"count"`
	}
	if err := cursor.All(ctx, &raw); err != nil {
		return nil, err
	}

	out := make([]TopReferrer, len(raw))
	for i, v := range raw {
		out[i] = TopReferrer{Referrer: v.ID, Count: v.Count}
	}
	return out, nil
}

func (r *MongoStatsRepo) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return r.client.Ping(pingCtx, nil)
}

func (r *MongoStatsRepo) Close(ctx context.Context) error {
	return r.client.Disconnect(ctx)
}

func (r *MongoStatsRepo) logSlow(ctx context.Context, start time.Time, code, queryType string) {
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		r.log.WarnContext(ctx, "slow query", "code", code, "query_type", queryType, "latency_ms", elapsed.Milliseconds())
	}
}
