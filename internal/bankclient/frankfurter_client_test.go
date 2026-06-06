package bankclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"currency-rate-aggregator/internal/domain"
)

func TestFrankfurterClientFetchRate(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if got, want := r.Header.Get("Accept"), "application/json"; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"base":"USD","quote":"RUB","date":"2026-06-05","rate":74.2956}`))
	}))
	defer server.Close()

	client := NewFrankfurterClient(server.URL+"/v2", server.Client())
	rate, err := client.FetchRate(context.Background(), " usd ")
	if err != nil {
		t.Fatalf("FetchRate() error = %v", err)
	}

	if gotPath != "/v2/rate/USD/RUB" {
		t.Fatalf("path = %q, want /v2/rate/USD/RUB", gotPath)
	}
	if rate.Currency != "USD" {
		t.Fatalf("Currency = %q, want USD", rate.Currency)
	}
	if rate.Bank != frankfurterBankName {
		t.Fatalf("Bank = %q, want %s", rate.Bank, frankfurterBankName)
	}
	if rate.Buy != 74.2956 || rate.Sell != 74.2956 {
		t.Fatalf("Buy/Sell = %v/%v, want 74.2956/74.2956", rate.Buy, rate.Sell)
	}
	wantFetchedAt := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	if !rate.FetchedAt.Equal(wantFetchedAt) {
		t.Fatalf("FetchedAt = %v, want %v", rate.FetchedAt, wantFetchedAt)
	}
}

func TestFrankfurterClientFetchRateReturnsRubBaseRate(t *testing.T) {
	client := NewFrankfurterClient("http://127.0.0.1/not-used", nil)
	now := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return now }

	rate, err := client.FetchRate(context.Background(), "rub")
	if err != nil {
		t.Fatalf("FetchRate() error = %v", err)
	}

	if rate.Currency != "RUB" || rate.Buy != 1 || rate.Sell != 1 {
		t.Fatalf("rate = %+v, want RUB 1/1", rate)
	}
	if !rate.FetchedAt.Equal(now) {
		t.Fatalf("FetchedAt = %v, want %v", rate.FetchedAt, now)
	}
}

func TestFrankfurterClientFetchRateReturnsCurrencyNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Could not find currency ABC"}`, http.StatusNotFound)
	}))
	defer server.Close()

	client := NewFrankfurterClient(server.URL, server.Client())
	_, err := client.FetchRate(context.Background(), "EUR")
	if !errors.Is(err, domain.ErrCurrencyNotFound) {
		t.Fatalf("FetchRate() error = %v, want ErrCurrencyNotFound", err)
	}
}

func TestFrankfurterClientFetchRateReportsHTTPErrorAsBankUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary failure", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewFrankfurterClient(server.URL, server.Client())
	_, err := client.FetchRate(context.Background(), "USD")
	if !errors.Is(err, domain.ErrBankUnavailable) {
		t.Fatalf("FetchRate() error = %v, want ErrBankUnavailable", err)
	}
}

func TestFrankfurterClientFetchRateReportsMalformedResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: `{`},
		{name: "zero rate", body: `{"base":"USD","quote":"RUB","date":"2026-06-05","rate":0}`},
		{name: "wrong base", body: `{"base":"EUR","quote":"RUB","date":"2026-06-05","rate":74.2956}`},
		{name: "wrong quote", body: `{"base":"USD","quote":"EUR","date":"2026-06-05","rate":74.2956}`},
		{name: "bad date", body: `{"base":"USD","quote":"RUB","date":"05.06.2026","rate":74.2956}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewFrankfurterClient(server.URL, server.Client())
			_, err := client.FetchRate(context.Background(), "USD")
			if !errors.Is(err, errFrankfurterMalformedResponse) {
				t.Fatalf("FetchRate() error = %v, want malformed response", err)
			}
		})
	}
}

func TestFrankfurterClientRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := NewFrankfurterClient("http://127.0.0.1/not-used", nil)
	_, err := client.FetchRate(ctx, "USD")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchRate() error = %v, want context.Canceled", err)
	}
}
