package config

import (
	"log/slog"
	"os"
	"os/exec"
	"testing"
)

var nopLog = slog.New(slog.NewTextHandler(os.Stderr, nil))

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("API_KEY", "testkey")
	os.Unsetenv("MONGO_URI")
	os.Unsetenv("MONGO_DB")
	os.Unsetenv("MONGO_COLLECTION")
	os.Unsetenv("PORT")
	os.Unsetenv("STATS_WINDOW_DAYS")
	os.Unsetenv("TOP_REFERRERS_LIMIT")

	cfg := Load(nopLog)

	if cfg.MongoURI != "mongodb://localhost:27017" {
		t.Errorf("MongoURI = %q, want default", cfg.MongoURI)
	}
	if cfg.MongoDB != "analytics" {
		t.Errorf("MongoDB = %q, want default", cfg.MongoDB)
	}
	if cfg.MongoCollection != "click_events" {
		t.Errorf("MongoCollection = %q, want default", cfg.MongoCollection)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want default", cfg.Port)
	}
	if cfg.StatsWindowDays != 30 {
		t.Errorf("StatsWindowDays = %d, want 30", cfg.StatsWindowDays)
	}
	if cfg.TopReferrersLimit != 10 {
		t.Errorf("TopReferrersLimit = %d, want 10", cfg.TopReferrersLimit)
	}
}

func TestLoad_InvalidPositiveInt_Exits(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string
		val  string
	}{
		{"zero STATS_WINDOW_DAYS", "STATS_WINDOW_DAYS", "0"},
		{"negative TOP_REFERRERS_LIMIT", "TOP_REFERRERS_LIMIT", "-1"},
		{"non-numeric STATS_WINDOW_DAYS", "STATS_WINDOW_DAYS", "abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if os.Getenv("TEST_SUBPROCESS") == "1" {
				os.Unsetenv("STATS_WINDOW_DAYS")
				os.Unsetenv("TOP_REFERRERS_LIMIT")
				t.Setenv(tc.env, tc.val)
				Load(nopLog)
				return
			}
			cmd := exec.Command(os.Args[0], "-test.run=TestLoad_InvalidPositiveInt_Exits/"+tc.name)
			cmd.Env = append(os.Environ(), "TEST_SUBPROCESS=1", "API_KEY=testkey", tc.env+"="+tc.val)
			err := cmd.Run()
			if err == nil {
				t.Fatalf("expected non-zero exit for %s=%s", tc.env, tc.val)
			}
		})
	}
}
