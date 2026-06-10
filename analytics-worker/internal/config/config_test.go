package config

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

var nopLog = slog.New(slog.NewTextHandler(os.Stderr, nil))

func TestLoad_Defaults(t *testing.T) {
	os.Unsetenv("AMQP_URL")
	os.Unsetenv("AMQP_PREFETCH")
	os.Unsetenv("MONGO_URI")
	os.Unsetenv("MONGO_DB")
	os.Unsetenv("HEALTH_PORT")
	os.Unsetenv("SHUTDOWN_TIMEOUT")

	cfg := Load(nopLog)

	if cfg.AMQPURL != "amqp://guest:guest@localhost:5672/" {
		t.Errorf("AMQPURL = %q, want default", cfg.AMQPURL)
	}
	if cfg.AMQPPrefetch != 10 {
		t.Errorf("AMQPPrefetch = %d, want 10", cfg.AMQPPrefetch)
	}
	if cfg.MongoURI != "mongodb://localhost:27017" {
		t.Errorf("MongoURI = %q, want default", cfg.MongoURI)
	}
	if cfg.MongoDB != "analytics" {
		t.Errorf("MongoDB = %q, want default", cfg.MongoDB)
	}
	if cfg.HealthPort != "8081" {
		t.Errorf("HealthPort = %q, want default", cfg.HealthPort)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 15s", cfg.ShutdownTimeout)
	}
}

func TestLoad_BadPrefetch_UsesDefault(t *testing.T) {
	t.Setenv("AMQP_PREFETCH", "not-an-int")

	cfg := Load(nopLog)

	if cfg.AMQPPrefetch != 10 {
		t.Errorf("AMQPPrefetch = %d, want 10 on bad parse", cfg.AMQPPrefetch)
	}
}

func TestLoad_BadDuration_UsesDefault(t *testing.T) {
	t.Setenv("SHUTDOWN_TIMEOUT", "not-a-duration")

	cfg := Load(nopLog)

	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 15s on bad parse", cfg.ShutdownTimeout)
	}
}
