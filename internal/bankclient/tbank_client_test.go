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

func TestTBankClientFetchRate(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if got, want := r.Header.Get("Accept"), "application/json"; got != want {
			require.FailNowf(t, "test failed", "Accept = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"resultCode":"OK",
			"payload":{
				"lastUpdate":{"milliseconds":1780685158794},
				"rates":[
					{"category":"DepositPayments","fromCurrency":{"name":"USD"},"toCurrency":{"name":"RUB"},"buy":69.9,"sell":80.05},
					{"category":"DebitCardsTransfers","fromCurrency":{"name":"USD"},"toCurrency":{"name":"RUB"},"buy":72.3,"sell":78.95}
				]
			}
		}`))
	}))
	defer server.Close()

	client := NewTBankClient(server.URL+"/rates", "DebitCardsTransfers", server.Client())
	rate, err := client.FetchRate(context.Background(), " usd ")
	require.NoErrorf(t, err,
		"FetchRate() error = %v", err)
	require.EqualValuesf(t, "/rates", gotPath,
		"path = %q, want /rates", gotPath)
	require.EqualValuesf(t, "USD", rate.Currency,
		"Currency = %q, want USD", rate.Currency)
	require.EqualValuesf(t, tbankBankName, rate.Bank,
		"Bank = %q, want %s", rate.Bank, tbankBankName)
	require.Falsef(t, rate.Buy != 72.3 || rate.Sell != 78.95,
		"Buy/Sell = %v/%v, want 72.3/78.95", rate.Buy, rate.Sell)

	wantFetchedAt := time.UnixMilli(1780685158794).UTC()
	require.Truef(t, rate.FetchedAt.Equal(wantFetchedAt),
		"FetchedAt = %v, want %v", rate.FetchedAt, wantFetchedAt)

}

func TestTBankClientFetchRateReturnsRubBaseRate(t *testing.T) {
	client := NewTBankClient("http://127.0.0.1/not-used", "", nil)
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

func TestTBankClientFetchRateReturnsCurrencyNotFound(t *testing.T) {
	tests := []struct {
		name     string
		category string
		body     string
	}{
		{
			name:     "currency missing",
			category: "DebitCardsTransfers",
			body:     `{"resultCode":"OK","payload":{"rates":[{"category":"DebitCardsTransfers","fromCurrency":{"name":"EUR"},"toCurrency":{"name":"RUB"},"buy":80,"sell":90}]}}`,
		},
		{
			name:     "category missing",
			category: "DebitCardsTransfers",
			body:     `{"resultCode":"OK","payload":{"rates":[{"category":"DepositPayments","fromCurrency":{"name":"USD"},"toCurrency":{"name":"RUB"},"buy":69,"sell":80}]}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewTBankClient(server.URL, tt.category, server.Client())
			_, err := client.FetchRate(context.Background(), "USD")
			require.ErrorIsf(t, err, domain.ErrCurrencyNotFound,
				"FetchRate() error = %v, want ErrCurrencyNotFound", err)

		})
	}
}

func TestTBankClientFetchRateReportsHTTPErrorAsBankUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary failure", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewTBankClient(server.URL, "", server.Client())
	_, err := client.FetchRate(context.Background(), "USD")
	require.ErrorIsf(t, err, domain.ErrBankUnavailable,
		"FetchRate() error = %v, want ErrBankUnavailable", err)

}

func TestTBankClientFetchRateReportsResultCodeAsBankUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"resultCode":"ERROR","payload":{"rates":[]}}`))
	}))
	defer server.Close()

	client := NewTBankClient(server.URL, "", server.Client())
	_, err := client.FetchRate(context.Background(), "USD")
	require.ErrorIsf(t, err, domain.ErrBankUnavailable,
		"FetchRate() error = %v, want ErrBankUnavailable", err)

}

func TestTBankClientFetchRateReportsMalformedResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: `{`},
		{name: "HTML block page", body: `<html><body><h1>Forbidden</h1></body></html>`},
		{name: "zero buy", body: `{"resultCode":"OK","payload":{"rates":[{"category":"DebitCardsTransfers","fromCurrency":{"name":"USD"},"toCurrency":{"name":"RUB"},"buy":0,"sell":78.95}]}}`},
		{name: "zero sell", body: `{"resultCode":"OK","payload":{"rates":[{"category":"DebitCardsTransfers","fromCurrency":{"name":"USD"},"toCurrency":{"name":"RUB"},"buy":72.3,"sell":0}]}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewTBankClient(server.URL, "", server.Client())
			_, err := client.FetchRate(context.Background(), "USD")
			require.ErrorIsf(t, err, errTBankMalformedResponse,
				"FetchRate() error = %v, want malformed response", err)

		})
	}
}

func TestTBankClientRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := NewTBankClient("http://127.0.0.1/not-used", "", nil)
	_, err := client.FetchRate(ctx, "USD")
	require.ErrorIsf(t, err, context.Canceled,
		"FetchRate() error = %v, want context.Canceled", err)

}
