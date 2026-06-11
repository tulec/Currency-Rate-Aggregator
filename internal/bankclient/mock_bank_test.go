package bankclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"currency-rate-aggregator/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestMockBankFetchRate(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	client := NewMockBank("Test Bank", map[string]domain.CurrencyRate{
		"usd": {Buy: 91.2, Sell: 92.1, FetchedAt: now},
	})

	rate, err := client.FetchRate(context.Background(), " USD ")
	require.NoErrorf(t, err,
		"FetchRate() error = %v", err)
	require.EqualValuesf(t, "USD", rate.Currency,
		"Currency = %q, want USD", rate.Currency)
	require.EqualValuesf(t, "Test Bank", rate.Bank,
		"Bank = %q, want Test Bank", rate.Bank)
	require.EqualValuesf(t, 91.2, rate.Buy,
		"Buy = %v, want 91.2", rate.Buy)
	require.EqualValuesf(t, 92.1, rate.Sell,
		"Sell = %v, want 92.1", rate.Sell)
	require.Truef(t, rate.FetchedAt.Equal(now),
		"FetchedAt = %v, want %v", rate.FetchedAt, now)

}

func TestMockBankFetchRateNormalizesFetchedAtToUTC(t *testing.T) {
	offset := time.FixedZone("MSK", 3*60*60)
	fetchedAt := time.Date(2026, 5, 18, 13, 0, 0, 0, offset)
	client := NewMockBank("Test Bank", map[string]domain.CurrencyRate{
		"USD": {Buy: 91.2, Sell: 92.1, FetchedAt: fetchedAt},
	})

	rate, err := client.FetchRate(context.Background(), "USD")
	require.NoErrorf(t, err,
		"FetchRate() error = %v", err)
	require.EqualValuesf(t, time.UTC, rate.FetchedAt.Location(),
		"FetchedAt location = %v, want UTC", rate.FetchedAt.Location())
	require.Truef(t, rate.FetchedAt.Equal(fetchedAt),
		"FetchedAt = %v, want same instant as %v", rate.FetchedAt, fetchedAt)

}

func TestMockBankFetchRateReturnsCurrencyError(t *testing.T) {
	client := NewMockBank("Test Bank", map[string]domain.CurrencyRate{
		"USD": {Buy: 91.2, Sell: 92.1},
	})

	_, err := client.FetchRate(context.Background(), "JPY")
	require.ErrorIsf(t, err, domain.ErrCurrencyNotFound,
		"FetchRate() error = %v, want ErrCurrencyNotFound", err)

}

func TestMockBankNilReceiverReturnsBankUnavailable(t *testing.T) {
	var client *MockBank

	if got := client.Name(); got != unknownBankName {
		require.FailNowf(t, "test failed", "Name() = %q, want %q", got, unknownBankName)
	}

	_, err := client.FetchRate(context.Background(), "USD")
	require.ErrorIsf(t, err, domain.ErrBankUnavailable,
		"FetchRate() error = %v, want ErrBankUnavailable", err)

}

func TestMockBankNilReceiverRespectsContextCancellation(t *testing.T) {
	var client *MockBank
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.FetchRate(ctx, "USD")
	require.ErrorIsf(t, err, context.Canceled,
		"FetchRate() error = %v, want context.Canceled", err)

}

func TestFailingMockBankReturnsBankError(t *testing.T) {
	client := NewFailingMockBank("Offline Bank", domain.ErrBankUnavailable)

	_, err := client.FetchRate(context.Background(), "USD")
	require.ErrorIsf(t, err, domain.ErrBankUnavailable,
		"FetchRate() error = %v, want ErrBankUnavailable", err)
	require.EqualValuesf(t, domain.ErrBankUnavailable.Error(), err.Error(),
		"FetchRate() error = %q, want %q", err.Error(), domain.ErrBankUnavailable.Error())

}

func TestFailingMockBankWrapsCustomErrorWithBankError(t *testing.T) {
	sourceErr := errors.New("maintenance")
	client := NewFailingMockBank("Offline Bank", sourceErr)

	_, err := client.FetchRate(context.Background(), "USD")
	require.ErrorIsf(t, err, domain.ErrBankUnavailable,
		"FetchRate() error = %v, want ErrBankUnavailable", err)
	require.ErrorIsf(t, err, sourceErr,
		"FetchRate() error = %v, want source error", err)

}

func TestMockBankRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := NewMockBank("Test Bank", map[string]domain.CurrencyRate{
		"USD": {Buy: 91.2, Sell: 92.1},
	})

	_, err := client.FetchRate(ctx, "USD")
	require.ErrorIsf(t, err, context.Canceled,
		"FetchRate() error = %v, want context.Canceled", err)

}

func TestDefaultMockBanksContainSuccessfulAndFailingClients(t *testing.T) {
	clients := DefaultMockBanks(time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC))
	require.Lenf(t, clients, 3,
		"len(DefaultMockBanks()) = %d, want 3", len(clients))

	var successCount int
	var failureCount int
	for _, client := range clients {
		_, err := client.FetchRate(context.Background(), "USD")
		if err != nil {
			failureCount++
			continue
		}
		successCount++
	}
	require.EqualValuesf(t, 2, successCount,
		"successCount = %d, want 2", successCount)
	require.EqualValuesf(t, 1, failureCount,
		"failureCount = %d, want 1", failureCount)

}

func TestDefaultLiveMockBanksUseFetchTime(t *testing.T) {
	clients := DefaultLiveMockBanks()

	before := time.Now().UTC()
	rate, err := clients[0].FetchRate(context.Background(), "USD")
	after := time.Now().UTC()
	require.NoErrorf(t, err,
		"FetchRate() error = %v", err)
	require.False(t, rate.FetchedAt.IsZero(),
		"FetchedAt is zero, want fetch timestamp")
	require.Falsef(t, rate.FetchedAt.Before(before) || rate.FetchedAt.After(after),
		"FetchedAt = %v, want between %v and %v", rate.FetchedAt, before, after)

}
