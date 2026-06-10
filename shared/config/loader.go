package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// Load selects and loads the appropriate .env file based on ENV (or ENVIRONMENT).
// Defaults to "local" if neither variable is set. Logs a warning and returns nil
// when the file does not exist, allowing production deployments with injected env vars.
func Load(serviceRoot string, log *slog.Logger) error {
	env := os.Getenv("ENV")
	if env == "" {
		env = os.Getenv("ENVIRONMENT")
	}
	if env == "" {
		env = "local"
	}

	filename, err := resolveEnvFile(env)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	path := filepath.Join(serviceRoot, filename)
	if err := godotenv.Load(path); err != nil {
		if os.IsNotExist(err) {
			log.Warn("env file not found, relying on injected environment variables", "path", path)
			return nil
		}
		return err
	}
	return nil
}

func resolveEnvFile(env string) (string, error) {
	switch env {
	case "local":
		return ".env", nil
	case "dev":
		return ".env.dev", nil
	case "prod":
		return ".env.prod", nil
	default:
		return "", fmt.Errorf("unknown ENV value: %s; expected local|dev|prod", env)
	}
}
