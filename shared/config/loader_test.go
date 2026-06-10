package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveEnvFile(t *testing.T) {
	tests := []struct {
		env      string
		want     string
		wantErr  bool
	}{
		{"local", ".env", false},
		{"dev", ".env.dev", false},
		{"prod", ".env.prod", false},
		{"unknown", "", true},
		{"staging", "", true},
	}
	for _, tt := range tests {
		got, err := resolveEnvFile(tt.env)
		if (err != nil) != tt.wantErr {
			t.Errorf("resolveEnvFile(%q) error = %v, wantErr %v", tt.env, err, tt.wantErr)
		}
		if got != tt.want {
			t.Errorf("resolveEnvFile(%q) = %q, want %q", tt.env, got, tt.want)
		}
	}
}

func TestLoad_MissingFile(t *testing.T) {
	t.Setenv("ENV", "local")
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	err := Load(t.TempDir(), log) // empty dir — no .env file
	if err != nil {
		t.Fatalf("expected nil error when file missing, got: %v", err)
	}
}

func TestLoad_LoadsVars(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("TEST_SHARED_VAR=hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ENV", "local")
	os.Unsetenv("TEST_SHARED_VAR")
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if err := Load(dir, log); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := os.Getenv("TEST_SHARED_VAR"); got != "hello" {
		t.Errorf("TEST_SHARED_VAR = %q, want %q", got, "hello")
	}
}
