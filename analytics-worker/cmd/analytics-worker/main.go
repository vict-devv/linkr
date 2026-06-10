package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	sharedconfig "github.com/linkr/shared/config"
	"github.com/linkr/analytics-worker/internal/config"
	"github.com/linkr/analytics-worker/internal/consumer"
	"github.com/linkr/analytics-worker/internal/handler"
	"github.com/linkr/analytics-worker/internal/repo"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := sharedconfig.Load(".", log); err != nil {
		log.Error("failed to load env file", "error", err)
		os.Exit(1)
	}

	cfg := config.Load(log)

	ctx := context.Background()

	mongoRepo, err := repo.NewMongoRepo(ctx, cfg.MongoURI, cfg.MongoDB, log)
	if err != nil {
		log.Error("failed to connect to mongodb", "error", err)
		os.Exit(1)
	}

	c := consumer.NewAMQPConsumer(cfg.AMQPURL, cfg.AMQPPrefetch, mongoRepo, log)
	healthSrv := handler.NewHealthServer(cfg.HealthPort, c.IsAlive, mongoRepo.Ping, log)

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := c.Start(sigCtx); err != nil {
		log.Error("failed to start amqp consumer", "error", err)
		os.Exit(1)
	}

	go func() {
		log.Info("health server starting", "port", cfg.HealthPort)
		if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("health server error", "error", err)
		}
	}()

	<-sigCtx.Done()
	log.Info("shutdown signal received")

	time.AfterFunc(cfg.ShutdownTimeout, func() {
		log.Error("shutdown timeout exceeded, forcing exit")
		os.Exit(1)
	})

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
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
