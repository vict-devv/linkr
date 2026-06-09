package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/linkr/analytics-worker/internal/consumer"
	"github.com/linkr/analytics-worker/internal/handler"
	"github.com/linkr/analytics-worker/internal/repo"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	amqpURL := envOr("AMQP_URL", "amqp://guest:guest@localhost:5672/")
	amqpPrefetch := parseIntOr(envOr("AMQP_PREFETCH", "10"), 10, log)
	mongoURI := envOr("MONGO_URI", "mongodb://localhost:27017")
	mongoDB := envOr("MONGO_DB", "analytics")
	healthPort := envOr("HEALTH_PORT", "8081")
	shutdownTimeout := parseDuration(envOr("SHUTDOWN_TIMEOUT", "15s"), log)

	ctx := context.Background()

	mongoRepo, err := repo.NewMongoRepo(ctx, mongoURI, mongoDB, log)
	if err != nil {
		log.Error("failed to connect to mongodb", "error", err)
		os.Exit(1)
	}

	c := consumer.NewAMQPConsumer(amqpURL, amqpPrefetch, mongoRepo, log)
	healthSrv := handler.NewHealthServer(healthPort, c.IsAlive, mongoRepo.Ping, log)

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := c.Start(sigCtx); err != nil {
		log.Error("failed to start amqp consumer", "error", err)
		os.Exit(1)
	}

	go func() {
		log.Info("health server starting", "port", healthPort)
		if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("health server error", "error", err)
		}
	}()

	<-sigCtx.Done()
	log.Info("shutdown signal received")

	time.AfterFunc(shutdownTimeout, func() {
		log.Error("shutdown timeout exceeded, forcing exit")
		os.Exit(1)
	})

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := c.Stop(); err != nil {
		log.Warn("consumer stop error", "error", err)
	}
	log.Info("amqp consumer stopped")

	if err := mongoRepo.Close(shutdownCtx); err != nil {
		log.Warn("mongo close error", "error", err)
	}
	log.Info("mongo connection closed")

	if err := healthSrv.Shutdown(shutdownCtx); err != nil {
		log.Warn("health server shutdown error", "error", err)
	}
	log.Info("health server stopped")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseIntOr(s string, def int, log *slog.Logger) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		log.Warn("invalid integer value, using default", "value", s, "default", def)
		return def
	}
	return n
}

func parseDuration(s string, log *slog.Logger) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Warn("invalid duration value, using default 15s", "value", s, "error", err)
		return 15 * time.Second
	}
	return d
}
