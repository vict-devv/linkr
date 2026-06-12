package config

import (
	"log/slog"
	"os"
	"os/exec"
	"testing"
	"time"
)

var nopLog = slog.New(slog.NewTextHandler(os.Stderr, nil))

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("REDIS_URL", "localhost:6379")
	t.Setenv("API_KEY", "testkey")
	os.Unsetenv("AMQP_URL")
	os.Unsetenv("HOST")
	os.Unsetenv("PORT")
	os.Unsetenv("CACHE_TTL")

	cfg := Load(nopLog)

	if cfg.AMQPURL != "amqp://guest:guest@localhost:5672/" {
		t.Errorf("AMQPURL = %q, want default", cfg.AMQPURL)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host = %q, want default", cfg.Host)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want default", cfg.Port)
	}
	if cfg.CacheTTL != 24*time.Hour {
		t.Errorf("CacheTTL = %v, want 24h", cfg.CacheTTL)
	}
}

func TestLoad_BadCacheTTL_UsesDefault(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("REDIS_URL", "localhost:6379")
	t.Setenv("API_KEY", "testkey")
	t.Setenv("CACHE_TTL", "not-a-duration")

	cfg := Load(nopLog)

	if cfg.CacheTTL != 24*time.Hour {
		t.Errorf("CacheTTL = %v, want 24h on bad parse", cfg.CacheTTL)
	}
}

func TestLoad_MissingRequired_Exits(t *testing.T) {
	if os.Getenv("TEST_SUBPROCESS") == "1" {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("REDIS_URL")
		Load(nopLog)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestLoad_MissingRequired_Exits")
	cmd.Env = append(os.Environ(), "TEST_SUBPROCESS=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit when required vars missing")
	}
}
