package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPMetricsWritesPrometheusCountersAndDurations(t *testing.T) {
	metrics := NewHTTPMetrics()
	metrics.ObserveRequest(http.MethodGet, "/health", http.StatusOK, 20*time.Millisecond)
	metrics.ObserveRequest(http.MethodGet, "/health", http.StatusOK, 30*time.Millisecond)
	metrics.ObserveRequest(http.MethodPost, "/health", http.StatusMethodNotAllowed, 12*time.Millisecond)
	metrics.ObserveCacheHit("USD")
	metrics.ObserveCacheMiss("EUR")
	metrics.ObserveCacheMiss("EUR")
	metrics.ObserveBankRequestError("Offline Bank")

	var body strings.Builder
	if err := metrics.WritePrometheus(&body); err != nil {
		t.Fatalf("WritePrometheus() error = %v", err)
	}

	output := body.String()
	assertContains(t, output, "# TYPE http_requests_total counter")
	assertContains(t, output, `http_requests_total{method="GET",path="/health",status="200"} 2`)
	assertContains(t, output, `http_requests_total{method="POST",path="/health",status="405"} 1`)
	assertContains(t, output, "# TYPE http_request_duration_seconds histogram")
	assertContains(t, output, `http_request_duration_seconds_bucket{method="GET",path="/health",status="200",le="0.05"} 2`)
	assertContains(t, output, `http_request_duration_seconds_bucket{method="GET",path="/health",status="200",le="+Inf"} 2`)
	assertContains(t, output, `http_request_duration_seconds_count{method="GET",path="/health",status="200"} 2`)
	assertContains(t, output, "# TYPE rate_cache_hits_total counter")
	assertContains(t, output, `rate_cache_hits_total{currency="USD"} 1`)
	assertContains(t, output, "# TYPE rate_cache_misses_total counter")
	assertContains(t, output, `rate_cache_misses_total{currency="EUR"} 2`)
	assertContains(t, output, "# TYPE bank_request_errors_total counter")
	assertContains(t, output, `bank_request_errors_total{bank="Offline Bank"} 1`)
}

func TestHTTPMetricsHandlerReturnsPrometheusText(t *testing.T) {
	metrics := NewHTTPMetrics()
	metrics.ObserveRequest(http.MethodGet, "/rates", http.StatusBadRequest, time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	metrics.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; version=0.0.4; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want Prometheus text", got)
	}
	assertContains(t, rec.Body.String(), `http_requests_total{method="GET",path="/rates",status="400"} 1`)
}

func TestHTTPMetricsEscapesLabelValues(t *testing.T) {
	metrics := NewHTTPMetrics()
	metrics.ObserveRequest(`GET"`, `/rates\special`, http.StatusOK, time.Millisecond)
	metrics.ObserveCacheHit(`U"S`)
	metrics.ObserveBankRequestError("Bank \"A\"\nNorth")

	var body strings.Builder
	if err := metrics.WritePrometheus(&body); err != nil {
		t.Fatalf("WritePrometheus() error = %v", err)
	}

	output := body.String()
	assertContains(t, output, `http_requests_total{method="GET\"",path="/rates\\special",status="200"} 1`)
	assertContains(t, output, `rate_cache_hits_total{currency="U\"S"} 1`)
	assertContains(t, output, `bank_request_errors_total{bank="Bank \"A\"\nNorth"} 1`)
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()

	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected output to contain %q, got:\n%s", needle, haystack)
	}
}
