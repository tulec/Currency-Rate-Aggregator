package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

var defaultHTTPRequestDurationBuckets = []float64{
	0.005,
	0.01,
	0.025,
	0.05,
	0.1,
	0.25,
	0.5,
	1,
	2.5,
	5,
	10,
}

type HTTPMetrics struct {
	mu                sync.Mutex
	buckets           []float64
	series            map[httpLabels]*httpSeries
	cacheHits         map[string]uint64
	cacheMisses       map[string]uint64
	bankRequestErrors map[string]uint64
}

type httpLabels struct {
	method string
	path   string
	status string
}

type httpSeries struct {
	count   uint64
	sum     float64
	buckets []uint64
}

func NewHTTPMetrics() *HTTPMetrics {
	buckets := append([]float64(nil), defaultHTTPRequestDurationBuckets...)
	return &HTTPMetrics{
		buckets:           buckets,
		series:            make(map[httpLabels]*httpSeries),
		cacheHits:         make(map[string]uint64),
		cacheMisses:       make(map[string]uint64),
		bankRequestErrors: make(map[string]uint64),
	}
}

func (m *HTTPMetrics) ObserveRequest(method, path string, status int, duration time.Duration) {
	if m == nil {
		return
	}

	seconds := duration.Seconds()
	labels := httpLabels{
		method: method,
		path:   path,
		status: strconv.Itoa(status),
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	series := m.series[labels]
	if series == nil {
		series = &httpSeries{buckets: make([]uint64, len(m.buckets))}
		m.series[labels] = series
	}

	series.count++
	series.sum += seconds
	for i, upperBound := range m.buckets {
		if seconds <= upperBound {
			series.buckets[i]++
		}
	}
}

func (m *HTTPMetrics) ObserveCacheHit(currency string) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.cacheHits[currency]++
}

func (m *HTTPMetrics) ObserveCacheMiss(currency string) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.cacheMisses[currency]++
}

func (m *HTTPMetrics) ObserveBankRequestError(bank string) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.bankRequestErrors[bank]++
}

func (m *HTTPMetrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = m.WritePrometheus(w)
}

func (m *HTTPMetrics) WritePrometheus(w io.Writer) error {
	if m == nil {
		return nil
	}

	snapshot := m.snapshot()

	if _, err := io.WriteString(w, "# HELP http_requests_total Total HTTP requests processed.\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "# TYPE http_requests_total counter\n"); err != nil {
		return err
	}
	for _, item := range snapshot.httpSeries {
		if _, err := fmt.Fprintf(w, "http_requests_total{method=%s,path=%s,status=%s} %d\n",
			labelValue(item.labels.method),
			labelValue(item.labels.path),
			labelValue(item.labels.status),
			item.series.count,
		); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(w, "# HELP http_request_duration_seconds HTTP request duration in seconds.\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "# TYPE http_request_duration_seconds histogram\n"); err != nil {
		return err
	}
	for _, item := range snapshot.httpSeries {
		for i, upperBound := range m.buckets {
			if _, err := fmt.Fprintf(w, "http_request_duration_seconds_bucket{method=%s,path=%s,status=%s,le=%s} %d\n",
				labelValue(item.labels.method),
				labelValue(item.labels.path),
				labelValue(item.labels.status),
				labelValue(strconv.FormatFloat(upperBound, 'g', -1, 64)),
				item.series.buckets[i],
			); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "http_request_duration_seconds_bucket{method=%s,path=%s,status=%s,le=%s} %d\n",
			labelValue(item.labels.method),
			labelValue(item.labels.path),
			labelValue(item.labels.status),
			labelValue("+Inf"),
			item.series.count,
		); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "http_request_duration_seconds_sum{method=%s,path=%s,status=%s} %g\n",
			labelValue(item.labels.method),
			labelValue(item.labels.path),
			labelValue(item.labels.status),
			item.series.sum,
		); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "http_request_duration_seconds_count{method=%s,path=%s,status=%s} %d\n",
			labelValue(item.labels.method),
			labelValue(item.labels.path),
			labelValue(item.labels.status),
			item.series.count,
		); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(w, "# HELP rate_cache_hits_total Total successful rate cache lookups.\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "# TYPE rate_cache_hits_total counter\n"); err != nil {
		return err
	}
	for _, item := range snapshot.cacheHits {
		if _, err := fmt.Fprintf(w, "rate_cache_hits_total{currency=%s} %d\n",
			labelValue(item.label),
			item.count,
		); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(w, "# HELP rate_cache_misses_total Total missed rate cache lookups.\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "# TYPE rate_cache_misses_total counter\n"); err != nil {
		return err
	}
	for _, item := range snapshot.cacheMisses {
		if _, err := fmt.Fprintf(w, "rate_cache_misses_total{currency=%s} %d\n",
			labelValue(item.label),
			item.count,
		); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(w, "# HELP bank_request_errors_total Total bank source request errors.\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "# TYPE bank_request_errors_total counter\n"); err != nil {
		return err
	}
	for _, item := range snapshot.bankRequestErrors {
		if _, err := fmt.Fprintf(w, "bank_request_errors_total{bank=%s} %d\n",
			labelValue(item.label),
			item.count,
		); err != nil {
			return err
		}
	}

	return nil
}

type snapshot struct {
	httpSeries        []snapshotItem
	cacheHits         []counterItem
	cacheMisses       []counterItem
	bankRequestErrors []counterItem
}

type snapshotItem struct {
	labels httpLabels
	series httpSeries
}

type counterItem struct {
	label string
	count uint64
}

func (m *HTTPMetrics) snapshot() snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := make([]snapshotItem, 0, len(m.series))
	for labels, series := range m.series {
		items = append(items, snapshotItem{
			labels: labels,
			series: httpSeries{
				count:   series.count,
				sum:     series.sum,
				buckets: append([]uint64(nil), series.buckets...),
			},
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].labels.method != items[j].labels.method {
			return items[i].labels.method < items[j].labels.method
		}
		if items[i].labels.path != items[j].labels.path {
			return items[i].labels.path < items[j].labels.path
		}
		return items[i].labels.status < items[j].labels.status
	})

	return snapshot{
		httpSeries:        items,
		cacheHits:         counterSnapshot(m.cacheHits),
		cacheMisses:       counterSnapshot(m.cacheMisses),
		bankRequestErrors: counterSnapshot(m.bankRequestErrors),
	}
}

func counterSnapshot(counters map[string]uint64) []counterItem {
	items := make([]counterItem, 0, len(counters))
	for label, count := range counters {
		items = append(items, counterItem{label: label, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].label < items[j].label
	})
	return items
}

func labelValue(value string) string {
	return strconv.Quote(value)
}
