package config

import (
	"log/slog"
	"os"
	"strconv"
)

type Config struct {
	MongoURI          string
	MongoDB           string
	MongoCollection   string
	Port              string
	StatsWindowDays   int
	TopReferrersLimit int
	APIKey            string
}

func Load(log *slog.Logger) Config {
	return Config{
		MongoURI:          envOr("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:           envOr("MONGO_DB", "analytics"),
		MongoCollection:   envOr("MONGO_COLLECTION", "click_events"),
		Port:              envOr("PORT", "8080"),
		StatsWindowDays:   parsePositiveInt("STATS_WINDOW_DAYS", 30, log),
		TopReferrersLimit: parsePositiveInt("TOP_REFERRERS_LIMIT", 10, log),
		APIKey:            mustEnv("API_KEY", log),
	}
}

func mustEnv(key string, log *slog.Logger) string {
	v := os.Getenv(key)
	if v == "" {
		log.Error("required environment variable not set", "key", key)
		os.Exit(1)
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parsePositiveInt(key string, def int, log *slog.Logger) int {
	s := envOr(key, strconv.Itoa(def))
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		log.Error("invalid environment variable: must be a positive integer", "key", key, "value", s)
		os.Exit(1)
	}
	return n
}
