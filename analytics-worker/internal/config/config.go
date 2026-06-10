package config

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

type Config struct {
	AMQPURL         string
	AMQPPrefetch    int
	MongoURI        string
	MongoDB         string
	HealthPort      string
	ShutdownTimeout time.Duration
}

func Load(log *slog.Logger) Config {
	return Config{
		AMQPURL:         envOr("AMQP_URL", "amqp://guest:guest@localhost:5672/"),
		AMQPPrefetch:    parseIntOr("AMQP_PREFETCH", 10, log),
		MongoURI:        envOr("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:         envOr("MONGO_DB", "analytics"),
		HealthPort:      envOr("HEALTH_PORT", "8081"),
		ShutdownTimeout: parseDuration("SHUTDOWN_TIMEOUT", "15s", log),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseIntOr(key string, def int, log *slog.Logger) int {
	s := envOr(key, strconv.Itoa(def))
	n, err := strconv.Atoi(s)
	if err != nil {
		log.Warn("invalid integer, using default", "key", key, "value", s, "default", def)
		return def
	}
	return n
}

func parseDuration(key, def string, log *slog.Logger) time.Duration {
	s := envOr(key, def)
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Warn("invalid duration, using default", "key", key, "value", s, "default", def)
		d, _ = time.ParseDuration(def)
	}
	return d
}
