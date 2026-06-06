package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/pprof"

	appmetrics "currency-rate-aggregator/internal/metrics"
	"currency-rate-aggregator/internal/ratelimit"
)

func NewRouter(rates rateFetcher, logger *slog.Logger) http.Handler {
	return NewRouterWithHistory(rates, nil, logger)
}

func NewRouterWithHistory(rates rateFetcher, history rateHistoryReader, logger *slog.Logger) http.Handler {
	return NewRouterWithHistoryAndRateLimit(rates, history, logger, nil)
}

func NewRouterWithHistoryAndRateLimit(rates rateFetcher, history rateHistoryReader, logger *slog.Logger, limiter *ratelimit.Limiter) http.Handler {
	return NewRouterWithHistoryRateLimitAndMetrics(rates, history, logger, limiter, appmetrics.NewHTTPMetrics())
}

func NewRouterWithHistoryRateLimitAndMetrics(rates rateFetcher, history rateHistoryReader, logger *slog.Logger, limiter *ratelimit.Limiter, metrics *appmetrics.HTTPMetrics) http.Handler {
	logger = loggerOrDiscard(logger)
	if metrics == nil {
		metrics = appmetrics.NewHTTPMetrics()
	}

	mux := http.NewServeMux()
	handlers := handlers{rates: rates, history: history}

	mux.HandleFunc("GET /health", healthHandler)
	mux.HandleFunc("GET /rates", handlers.ratesHandler)
	mux.HandleFunc("GET /convert", handlers.convertHandler)
	mux.HandleFunc("GET /rates/history", handlers.ratesHistoryHandler)
	mux.HandleFunc("GET /rates/history/by-date", handlers.ratesHistoryByDateHandler)
	mux.Handle("GET /metrics", metrics)
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	mux.HandleFunc("/health", methodNotAllowedHandler)
	mux.HandleFunc("/rates", methodNotAllowedHandler)
	mux.HandleFunc("/convert", methodNotAllowedHandler)
	mux.HandleFunc("/rates/history", methodNotAllowedHandler)
	mux.HandleFunc("/rates/history/by-date", methodNotAllowedHandler)
	mux.HandleFunc("/metrics", methodNotAllowedHandler)
	mux.HandleFunc("/debug/pprof/", methodNotAllowedHandler)
	mux.HandleFunc("/", notFoundHandler)

	return requestMetrics(metrics)(requestLogger(logger)(rateLimitMiddleware(limiter)(mux)))
}
