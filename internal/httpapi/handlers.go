package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"currency-rate-aggregator/internal/domain"
	"currency-rate-aggregator/internal/service"
	"currency-rate-aggregator/internal/storage"
)

var errInvalidHistoryLimit = errors.New("invalid history limit")

const statusClientClosedRequest = 499
const allowedReadMethods = http.MethodGet + ", " + http.MethodHead

type rateFetcher interface {
	FetchRates(ctx context.Context, currency string) (domain.RateResult, error)
}

type rateHistoryReader interface {
	History(ctx context.Context, currency string, limit int) ([]domain.CurrencyRate, error)
}

type healthResponse struct {
	Status string `json:"status"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type handlers struct {
	rates   rateFetcher
	history rateHistoryReader
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func notFoundHandler(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "route not found")
}

func methodNotAllowedHandler(w http.ResponseWriter, r *http.Request) {
	if allow := allowedMethodsForPath(r.URL.Path); allow != "" {
		w.Header().Set("Allow", allow)
	}
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func allowedMethodsForPath(path string) string {
	switch path {
	case "/health",
		"/rates",
		"/rates/history",
		"/metrics":
		return allowedReadMethods
	}
	if strings.HasPrefix(path, "/debug/pprof/") {
		return allowedReadMethods
	}
	return ""
}

func (h handlers) ratesHandler(w http.ResponseWriter, r *http.Request) {
	currency := strings.TrimSpace(r.URL.Query().Get("currency"))
	if currency == "" {
		writeError(w, http.StatusBadRequest, "currency query parameter is required")
		return
	}
	normalized, err := domain.NormalizeCurrency(currency)
	if err != nil {
		writeError(w, http.StatusBadRequest, "currency must be a 3-letter code")
		return
	}

	if isNilDependency(h.rates) {
		writeError(w, http.StatusInternalServerError, "rates service is not configured")
		return
	}

	result, err := h.rates.FetchRates(r.Context(), normalized)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			writeError(w, statusClientClosedRequest, "request canceled")
		case errors.Is(err, context.DeadlineExceeded):
			writeError(w, http.StatusGatewayTimeout, "request timed out")
		case errors.Is(err, domain.ErrInvalidCurrencyCode):
			writeError(w, http.StatusBadRequest, "currency must be a 3-letter code")
		case errors.Is(err, service.ErrNoRatesAvailable):
			writeError(w, http.StatusServiceUnavailable, "no rates available")
		default:
			writeError(w, http.StatusInternalServerError, "failed to fetch rates")
		}
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h handlers) ratesHistoryHandler(w http.ResponseWriter, r *http.Request) {
	currency := strings.TrimSpace(r.URL.Query().Get("currency"))
	if currency == "" {
		writeError(w, http.StatusBadRequest, "currency query parameter is required")
		return
	}
	normalized, err := domain.NormalizeCurrency(currency)
	if err != nil {
		writeError(w, http.StatusBadRequest, "currency must be a 3-letter code")
		return
	}

	limit, err := parseHistoryLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "limit must be a positive integer")
		return
	}

	if isNilDependency(h.history) {
		writeError(w, http.StatusInternalServerError, "rate history store is not configured")
		return
	}

	rates, err := h.history.History(r.Context(), normalized, limit)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			writeError(w, statusClientClosedRequest, "request canceled")
		case errors.Is(err, context.DeadlineExceeded):
			writeError(w, http.StatusGatewayTimeout, "request timed out")
		case errors.Is(err, domain.ErrInvalidCurrencyCode):
			writeError(w, http.StatusBadRequest, "currency must be a 3-letter code")
		case errors.Is(err, storage.ErrStoreNotConfigured):
			writeError(w, http.StatusInternalServerError, "rate history store is not configured")
		default:
			writeError(w, http.StatusInternalServerError, "failed to fetch rate history")
		}
		return
	}
	if rates == nil {
		rates = []domain.CurrencyRate{}
	}

	writeJSON(w, http.StatusOK, rates)
}

func parseHistoryLimit(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return storage.DefaultHistoryLimit, nil
	}

	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return 0, errInvalidHistoryLimit
	}
	if limit > storage.MaxHistoryLimit {
		return storage.MaxHistoryLimit, nil
	}
	return limit, nil
}

func isNilDependency(value any) bool {
	if value == nil {
		return true
	}

	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}
