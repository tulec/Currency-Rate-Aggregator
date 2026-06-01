package httpapi

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	appmetrics "currency-rate-aggregator/internal/metrics"
	"currency-rate-aggregator/internal/ratelimit"
)

const unknownClientIP = "unknown"

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	logger = loggerOrDiscard(logger)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(recorder, r)

			logger.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", recorder.status),
				slog.Duration("duration", time.Since(started)),
			)
		})
	}
}

func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	logger = loggerOrDiscard(logger)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error("panic recovered",
						slog.Any("panic", recovered),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
						slog.Bool("response_started", recorder.wroteHeader),
					)
					if !recorder.wroteHeader {
						writeError(recorder, http.StatusInternalServerError, "internal server error")
					}
				}
			}()

			next.ServeHTTP(recorder, r)
		})
	}
}

func loggerOrDiscard(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return logger
}

func requestMetrics(metrics *appmetrics.HTTPMetrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if metrics == nil {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(recorder, r)

			metrics.ObserveRequest(r.Method, metricPathLabel(r, recorder.status), recorder.status, time.Since(started))
		})
	}
}

func rateLimitMiddleware(limiter *ratelimit.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if limiter == nil {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow(clientIP(r)) {
				w.Header().Set("Retry-After", strconv.Itoa(int(ratelimit.Window.Seconds())))
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	remoteAddr := strings.TrimSpace(r.RemoteAddr)
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil && host != "" {
		return host
	}
	if remoteAddr == "" {
		return unknownClientIP
	}
	return remoteAddr
}

func metricPathLabel(r *http.Request, status int) string {
	if r.Pattern != "" {
		if r.Pattern == "/" && status == http.StatusNotFound {
			return "unmatched"
		}
		return routePath(r.Pattern)
	}
	if path, ok := knownMetricPathLabel(r.URL.Path); ok {
		return path
	}
	if status == http.StatusNotFound || status == http.StatusTooManyRequests {
		return "unmatched"
	}
	return r.URL.Path
}

func knownMetricPathLabel(path string) (string, bool) {
	switch path {
	case "/health",
		"/rates",
		"/rates/history",
		"/metrics":
		return path, true
	}
	if strings.HasPrefix(path, "/debug/pprof/") {
		return "/debug/pprof/", true
	}
	return "", false
}

func routePath(pattern string) string {
	method, path, ok := strings.Cut(pattern, " ")
	if ok && isHTTPMethod(method) {
		return path
	}
	return pattern
}

func isHTTPMethod(value string) bool {
	switch value {
	case http.MethodConnect,
		http.MethodDelete,
		http.MethodGet,
		http.MethodHead,
		http.MethodOptions,
		http.MethodPatch,
		http.MethodPost,
		http.MethodPut,
		http.MethodTrace:
		return true
	default:
		return false
	}
}
