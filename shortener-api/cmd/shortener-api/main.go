package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	sharedconfig "github.com/linkr/shared/config"
	"github.com/linkr/shortener-api/internal/cache"
	"github.com/linkr/shortener-api/internal/config"
	"github.com/linkr/shortener-api/internal/handler"
	"github.com/linkr/shortener-api/internal/publisher"
	"github.com/linkr/shortener-api/internal/repo"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := sharedconfig.Load(".", log); err != nil {
		log.Error("failed to load env file", "error", err)
		os.Exit(1)
	}

	cfg := config.Load(log)

	ctx := context.Background()

	pgRepo, err := repo.NewPostgresRepo(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	redisCache := cache.NewRedisCache(cfg.RedisURL)

	pub := publisher.NewAMQPPublisher(cfg.AMQPURL, log)
	pub.Connect()

	routerCfg := handler.Config{
		Host:     cfg.Host,
		Port:     cfg.Port,
		CacheTTL: cfg.CacheTTL,
	}

	router := handler.NewRouter(routerCfg, pgRepo, redisCache, pgRepo.Ping, redisCache.Ping, pub, pub.IsAlive, log)

	addr := cfg.Host + ":" + cfg.Port
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		log.Info("starting server", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-sigCtx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("http server shutdown error", "error", err)
	}
	if err := pub.Close(); err != nil {
		log.Warn("publisher close error", "error", err)
	}
	log.Info("shutdown complete")
}
