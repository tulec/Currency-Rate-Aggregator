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

func TestCBRClientFetchRate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ValCurs Date="05.06.2026" name="Foreign Currency Market">
	<Valute ID="R01235">
		<CharCode>USD</CharCode>
		<Nominal>1</Nominal>
		<Value>74,2956</Value>
		<VunitRate>74,2956</VunitRate>
	</Valute>
</ValCurs>`))
	}))
	defer server.Close()

	client := NewCBRClient(server.URL, server.Client())
	rate, err := client.FetchRate(context.Background(), " usd ")
	require.NoErrorf(t, err,
		"FetchRate() error = %v", err)
	require.EqualValuesf(t, "USD", rate.Currency,
		"Currency = %q, want USD", rate.Currency)
	require.EqualValuesf(t, cbrBankName, rate.Bank,
		"Bank = %q, want %s", rate.Bank, cbrBankName)
	require.Falsef(t, rate.Buy != 74.2956 || rate.Sell != 74.2956,
		"Buy/Sell = %v/%v, want 74.2956/74.2956", rate.Buy, rate.Sell)

	wantFetchedAt := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	require.Truef(t, rate.FetchedAt.Equal(wantFetchedAt),
		"FetchedAt = %v, want %v", rate.FetchedAt, wantFetchedAt)

}

func TestCBRClientFetchRateUsesNominalWhenUnitRateMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<ValCurs Date="05.06.2026"><Valute><CharCode>KZT</CharCode><Nominal>100</Nominal><Value>13,5000</Value></Valute></ValCurs>`))
	}))
	defer server.Close()

	client := NewCBRClient(server.URL, server.Client())
	rate, err := client.FetchRate(context.Background(), "KZT")
	require.NoErrorf(t, err,
		"FetchRate() error = %v", err)
	require.Falsef(t, rate.Buy != 0.135 || rate.Sell != 0.135,
		"Buy/Sell = %v/%v, want 0.135/0.135", rate.Buy, rate.Sell)

}

func TestCBRClientFetchRateSupportsWindows1251XML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=windows-1251")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="windows-1251"?><ValCurs Date="05.06.2026"><Valute><CharCode>USD</CharCode><Nominal>1</Nominal><Name>` + string([]byte{0xc4, 0xee, 0xeb, 0xeb, 0xe0, 0xf0}) + `</Name><Value>74,2956</Value></Valute></ValCurs>`))
	}))
	defer server.Close()

	client := NewCBRClient(server.URL, server.Client())
	rate, err := client.FetchRate(context.Background(), "USD")
	require.NoErrorf(t, err,
		"FetchRate() error = %v", err)
	require.EqualValuesf(t, 74.2956, rate.Buy,
		"Buy = %v, want 74.2956", rate.Buy)

}

func TestCBRClientFetchRateReturnsRubBaseRate(t *testing.T) {
	client := NewCBRClient("http://127.0.0.1/not-used", nil)
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

func TestCBRClientFetchRateReturnsCurrencyNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<ValCurs Date="05.06.2026"><Valute><CharCode>USD</CharCode><Nominal>1</Nominal><Value>74,2956</Value></Valute></ValCurs>`))
	}))
	defer server.Close()

	client := NewCBRClient(server.URL, server.Client())
	_, err := client.FetchRate(context.Background(), "EUR")
	require.ErrorIsf(t, err, domain.ErrCurrencyNotFound,
		"FetchRate() error = %v, want ErrCurrencyNotFound", err)

}

func TestCBRClientFetchRateReportsHTTPErrorAsBankUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary failure", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewCBRClient(server.URL, server.Client())
	_, err := client.FetchRate(context.Background(), "USD")
	require.ErrorIsf(t, err, domain.ErrBankUnavailable,
		"FetchRate() error = %v, want ErrBankUnavailable", err)

}

func TestCBRClientFetchRateReportsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<ValCurs Date="bad"><Valute><CharCode>USD</CharCode><Nominal>1</Nominal><Value>74,2956</Value></Valute></ValCurs>`))
	}))
	defer server.Close()

	client := NewCBRClient(server.URL, server.Client())
	_, err := client.FetchRate(context.Background(), "USD")
	require.ErrorIsf(t, err, errCBRMalformedResponse,
		"FetchRate() error = %v, want malformed response", err)

}

func TestCBRClientRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := NewCBRClient("http://127.0.0.1/not-used", nil)
	_, err := client.FetchRate(ctx, "USD")
	require.ErrorIsf(t, err, context.Canceled,
		"FetchRate() error = %v, want context.Canceled", err)

}
