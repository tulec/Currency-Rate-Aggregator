package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"currency-rate-aggregator/internal/domain"
	"currency-rate-aggregator/internal/service"
	"currency-rate-aggregator/internal/storage"
)

var errInvalidHistoryLimit = errors.New("invalid history limit")

const statusClientClosedRequest = 499
const allowedReadMethods = http.MethodGet + ", " + http.MethodHead
const baseCurrency = "RUB"

type rateFetcher interface {
	FetchRates(ctx context.Context, currency string) (domain.RateResult, error)
}

type rateHistoryReader interface {
	History(ctx context.Context, currency string, limit int) ([]domain.CurrencyRate, error)
	HistoryByDate(ctx context.Context, currency string, from, to time.Time, limit int) ([]domain.CurrencyRate, error)
}

type healthResponse struct {
	Status string `json:"status"`
}

type conversionResponse struct {
	From            domain.CurrencyCode `json:"from"`
	To              domain.CurrencyCode `json:"to"`
	Amount          float64             `json:"amount"`
	ConvertedAmount float64             `json:"converted_amount"`
	Rate            float64             `json:"rate"`
}

type responseEnvelope struct {
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
	Meta  any    `json:"meta,omitempty"`
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
		"/rates/history/by-date",
		"/convert",
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

func (h handlers) convertHandler(w http.ResponseWriter, r *http.Request) {
	from, err := parseCurrencyQuery(r, "from")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	to, err := parseCurrencyQuery(r, "to")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	amount, err := parsePositiveAmount(r.URL.Query().Get("amount"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "amount must be a positive number")
		return
	}

	if from == to {
		writeJSON(w, http.StatusOK, conversionResponse{
			From:            domain.CurrencyCode(from),
			To:              domain.CurrencyCode(to),
			Amount:          amount,
			ConvertedAmount: amount,
			Rate:            1,
		})
		return
	}

	if isNilDependency(h.rates) {
		writeError(w, http.StatusInternalServerError, "rates service is not configured")
		return
	}

	converted, err := h.convertAmount(r.Context(), from, to, amount)
	if err != nil {
		h.writeRatesError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, conversionResponse{
		From:            domain.CurrencyCode(from),
		To:              domain.CurrencyCode(to),
		Amount:          amount,
		ConvertedAmount: converted,
		Rate:            converted / amount,
	})
}

func (h handlers) convertAmount(ctx context.Context, from, to string, amount float64) (float64, error) {
	rubles := amount
	if from != baseCurrency {
		fromRates, err := h.rates.FetchRates(ctx, from)
		if err != nil {
			return 0, err
		}
		rubles = amount * fromRates.BestBuy.Buy
	}

	if to == baseCurrency {
		return rubles, nil
	}

	toRates, err := h.rates.FetchRates(ctx, to)
	if err != nil {
		return 0, err
	}
	return rubles / toRates.BestSell.Sell, nil
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

func (h handlers) ratesHistoryByDateHandler(w http.ResponseWriter, r *http.Request) {
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

	from, to, err := parseHistoryDateRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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

	rates, err := h.history.HistoryByDate(r.Context(), normalized, from, to, limit)
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

func parseCurrencyQuery(r *http.Request, name string) (string, error) {
	value := strings.TrimSpace(r.URL.Query().Get(name))
	if value == "" {
		return "", errors.New(name + " query parameter is required")
	}
	normalized, err := domain.NormalizeCurrency(value)
	if err != nil {
		return "", errors.New(name + " must be a 3-letter code")
	}
	return normalized, nil
}

func parsePositiveAmount(value string) (float64, error) {
	amount, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, errors.New("invalid amount")
	}
	return amount, nil
}

func (h handlers) writeRatesError(w http.ResponseWriter, err error) {
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

func parseHistoryDateRange(fromValue, toValue string) (time.Time, time.Time, error) {
	fromValue = strings.TrimSpace(fromValue)
	toValue = strings.TrimSpace(toValue)
	if fromValue == "" {
		return time.Time{}, time.Time{}, errors.New("from query parameter is required")
	}
	if toValue == "" {
		return time.Time{}, time.Time{}, errors.New("to query parameter is required")
	}

	from, _, err := parseHistoryDate("from", fromValue, false)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, toWasDateOnly, err := parseHistoryDate("to", toValue, true)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if toWasDateOnly {
		to = to.AddDate(0, 0, 1)
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, errors.New("from must be before to")
	}
	return from, to, nil
}

func parseHistoryDate(name, value string, allowDateOnly bool) (time.Time, bool, error) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), false, nil
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.UTC(), true, nil
	}
	if allowDateOnly {
		return time.Time{}, false, errors.New(name + " must be YYYY-MM-DD or RFC3339")
	}
	return time.Time{}, false, errors.New(name + " must be YYYY-MM-DD or RFC3339")
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
	_ = json.NewEncoder(w).Encode(responseEnvelope{Data: value})
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(responseEnvelope{Error: message})
}
