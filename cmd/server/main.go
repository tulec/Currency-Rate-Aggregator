package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"currency-rate-aggregator/internal/bankclient"
	"currency-rate-aggregator/internal/cache"
	"currency-rate-aggregator/internal/config"
	"currency-rate-aggregator/internal/httpapi"
	appmetrics "currency-rate-aggregator/internal/metrics"
	"currency-rate-aggregator/internal/ratelimit"
	"currency-rate-aggregator/internal/service"
	"currency-rate-aggregator/internal/storage"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, closeStore, err := storage.OpenPostgresStore(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("setup postgres store", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		if err := closeStore(); err != nil {
			logger.Error("close postgres store", slog.String("error", err.Error()))
		}
	}()

	clients := bankclient.DefaultLiveMockBanks()
	rateCache := cache.NewTTLCache(cfg.CacheTTL)
	metrics := appmetrics.NewHTTPMetrics()
	aggregator := service.NewAggregatorWithCacheAndStore(clients, rateCache, store).
		WithMetrics(metrics).
		WithLogger(logger)

	scheduler, err := service.NewScheduler(aggregator, cfg.SchedulerCurrencies, cfg.SchedulerInterval, logger)
	if err != nil {
		logger.Error("setup scheduler", slog.String("error", err.Error()))
		os.Exit(1)
	}
	limiter := ratelimit.New(cfg.RateLimitRPM)

	server := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      httpapi.NewRouterWithHistoryRateLimitAndMetrics(aggregator, store, logger, limiter, metrics),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		logger.Info("scheduler started",
			slog.Duration("interval", cfg.SchedulerInterval),
			slog.Any("currencies", cfg.SchedulerCurrencies),
		)
		scheduler.Run(ctx)
	}()

	go func() {
		logger.Info("server started", slog.String("addr", cfg.HTTPAddr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", slog.String("error", err.Error()))
			stop()
		}
	}()

	<-ctx.Done()
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	logger.Info("server stopping", slog.Duration("timeout", cfg.ShutdownTimeout))
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", slog.String("error", err.Error()))
		if closeErr := server.Close(); closeErr != nil {
			logger.Error("server close failed", slog.String("error", closeErr.Error()))
		}
	}

	select {
	case <-schedulerDone:
	case <-shutdownCtx.Done():
		logger.Error("scheduler shutdown timed out", slog.String("error", shutdownCtx.Err().Error()))
	}

	logger.Info("server stopped")
}
