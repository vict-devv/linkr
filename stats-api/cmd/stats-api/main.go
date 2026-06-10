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
	"github.com/linkr/stats-api/internal/config"
	"github.com/linkr/stats-api/internal/handler"
	"github.com/linkr/stats-api/internal/repo"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := sharedconfig.Load(".", log); err != nil {
		log.Error("failed to load env file", "error", err)
		os.Exit(1)
	}

	cfg := config.Load(log)

	ctx := context.Background()

	mongoRepo, err := repo.NewMongoStatsRepo(ctx, cfg.MongoURI, cfg.MongoDB, cfg.MongoCollection, log)
	if err != nil {
		log.Error("failed to connect to mongodb", "error", err)
		os.Exit(1)
	}

	routerCfg := handler.Config{
		Port:              cfg.Port,
		StatsWindowDays:   cfg.StatsWindowDays,
		TopReferrersLimit: cfg.TopReferrersLimit,
	}

	router := handler.NewRouter(routerCfg, mongoRepo, mongoRepo.Ping, log)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		log.Info("starting server", "addr", srv.Addr)
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
	if err := mongoRepo.Close(shutdownCtx); err != nil {
		log.Warn("mongo close error", "error", err)
	}
	log.Info("shutdown complete")
}
