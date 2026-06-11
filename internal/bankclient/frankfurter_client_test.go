package bankclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"currency-rate-aggregator/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestFrankfurterClientFetchRate(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if got, want := r.Header.Get("Accept"), "application/json"; got != want {
			require.FailNowf(t, "test failed", "Accept = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"base":"USD","quote":"RUB","date":"2026-06-05","rate":74.2956}`))
	}))
	defer server.Close()

	client := NewFrankfurterClient(server.URL+"/v2", server.Client())
	rate, err := client.FetchRate(context.Background(), " usd ")
	require.NoErrorf(t, err,
		"FetchRate() error = %v", err)
	require.EqualValuesf(t, "/v2/rate/USD/RUB", gotPath,
		"path = %q, want /v2/rate/USD/RUB", gotPath)
	require.EqualValuesf(t, "USD", rate.Currency,
		"Currency = %q, want USD", rate.Currency)
	require.EqualValuesf(t, frankfurterBankName, rate.Bank,
		"Bank = %q, want %s", rate.Bank, frankfurterBankName)
	require.Falsef(t, rate.Buy != 74.2956 || rate.Sell != 74.2956,
		"Buy/Sell = %v/%v, want 74.2956/74.2956", rate.Buy, rate.Sell)

	wantFetchedAt := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	require.Truef(t, rate.FetchedAt.Equal(wantFetchedAt),
		"FetchedAt = %v, want %v", rate.FetchedAt, wantFetchedAt)

}

func TestFrankfurterClientFetchRateReturnsRubBaseRate(t *testing.T) {
	client := NewFrankfurterClient("http://127.0.0.1/not-used", nil)
	now := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return now }

	rate, err := client.FetchRate(context.Background(), "rub")
	require.NoErrorf(t, err,
		"FetchRate() error = %v", err)
	require.Falsef(t, rate.Currency != "RUB" || rate.Buy != 1 || rate.Sell != 1,
		"rate = %+v, want RUB 1/1", rate)
	require.Truef(t, rate.FetchedAt.Equal(now),
		"FetchedAt = %v, want %v", rate.FetchedAt, now)

}

func TestFrankfurterClientFetchRateReturnsCurrencyNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Could not find currency ABC"}`, http.StatusNotFound)
	}))
	defer server.Close()

	client := NewFrankfurterClient(server.URL, server.Client())
	_, err := client.FetchRate(context.Background(), "EUR")
	require.ErrorIsf(t, err, domain.ErrCurrencyNotFound,
		"FetchRate() error = %v, want ErrCurrencyNotFound", err)

}

func TestFrankfurterClientFetchRateReportsHTTPErrorAsBankUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary failure", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewFrankfurterClient(server.URL, server.Client())
	_, err := client.FetchRate(context.Background(), "USD")
	require.ErrorIsf(t, err, domain.ErrBankUnavailable,
		"FetchRate() error = %v, want ErrBankUnavailable", err)

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
			require.ErrorIsf(t, err, errFrankfurterMalformedResponse,
				"FetchRate() error = %v, want malformed response", err)

		})
	}
}

func TestFrankfurterClientRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := NewFrankfurterClient("http://127.0.0.1/not-used", nil)
	_, err := client.FetchRate(ctx, "USD")
	require.ErrorIsf(t, err, context.Canceled,
		"FetchRate() error = %v, want context.Canceled", err)

}
