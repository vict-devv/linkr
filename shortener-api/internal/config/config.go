package config

import (
	"log/slog"
	"os"
	"time"
)

type Config struct {
	DatabaseURL string
	RedisURL    string
	AMQPURL     string
	Host        string
	Port        string
	CacheTTL    time.Duration
	APIKey      string
}

func Load(log *slog.Logger) Config {
	return Config{
		DatabaseURL: mustEnv("DATABASE_URL", log),
		RedisURL:    mustEnv("REDIS_URL", log),
		AMQPURL:     envOr("AMQP_URL", "amqp://guest:guest@localhost:5672/"),
		Host:        envOr("HOST", "0.0.0.0"),
		Port:        envOr("PORT", "8080"),
		CacheTTL:    parseDuration("CACHE_TTL", "24h", log),
		APIKey:      mustEnv("API_KEY", log),
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

func parseDuration(key, def string, log *slog.Logger) time.Duration {
	s := envOr(key, def)
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Warn("invalid duration, using default", "key", key, "value", s, "default", def)
		d, _ = time.ParseDuration(def)
	}
	return d
}
