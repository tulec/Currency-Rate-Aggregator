package bankclient

import (
	"context"
	"errors"
	"testing"
	"time"

	"currency-rate-aggregator/internal/domain"
)

func TestMockBankFetchRate(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	client := NewMockBank("Test Bank", map[string]domain.CurrencyRate{
		"usd": {Buy: 91.2, Sell: 92.1, FetchedAt: now},
	})

	rate, err := client.FetchRate(context.Background(), " USD ")
	if err != nil {
		t.Fatalf("FetchRate() error = %v", err)
	}

	if rate.Currency != "USD" {
		t.Fatalf("Currency = %q, want USD", rate.Currency)
	}
	if rate.Bank != "Test Bank" {
		t.Fatalf("Bank = %q, want Test Bank", rate.Bank)
	}
	if rate.Buy != 91.2 {
		t.Fatalf("Buy = %v, want 91.2", rate.Buy)
	}
	if rate.Sell != 92.1 {
		t.Fatalf("Sell = %v, want 92.1", rate.Sell)
	}
	if !rate.FetchedAt.Equal(now) {
		t.Fatalf("FetchedAt = %v, want %v", rate.FetchedAt, now)
	}
}

func TestMockBankFetchRateNormalizesFetchedAtToUTC(t *testing.T) {
	offset := time.FixedZone("MSK", 3*60*60)
	fetchedAt := time.Date(2026, 5, 18, 13, 0, 0, 0, offset)
	client := NewMockBank("Test Bank", map[string]domain.CurrencyRate{
		"USD": {Buy: 91.2, Sell: 92.1, FetchedAt: fetchedAt},
	})

	rate, err := client.FetchRate(context.Background(), "USD")
	if err != nil {
		t.Fatalf("FetchRate() error = %v", err)
	}

	if rate.FetchedAt.Location() != time.UTC {
		t.Fatalf("FetchedAt location = %v, want UTC", rate.FetchedAt.Location())
	}
	if !rate.FetchedAt.Equal(fetchedAt) {
		t.Fatalf("FetchedAt = %v, want same instant as %v", rate.FetchedAt, fetchedAt)
	}
}

func TestMockBankFetchRateReturnsCurrencyError(t *testing.T) {
	client := NewMockBank("Test Bank", map[string]domain.CurrencyRate{
		"USD": {Buy: 91.2, Sell: 92.1},
	})

	_, err := client.FetchRate(context.Background(), "JPY")
	if !errors.Is(err, domain.ErrCurrencyNotFound) {
		t.Fatalf("FetchRate() error = %v, want ErrCurrencyNotFound", err)
	}
}

func TestMockBankNilReceiverReturnsBankUnavailable(t *testing.T) {
	var client *MockBank

	if got := client.Name(); got != unknownBankName {
		t.Fatalf("Name() = %q, want %q", got, unknownBankName)
	}

	_, err := client.FetchRate(context.Background(), "USD")
	if !errors.Is(err, domain.ErrBankUnavailable) {
		t.Fatalf("FetchRate() error = %v, want ErrBankUnavailable", err)
	}
}

func TestMockBankNilReceiverRespectsContextCancellation(t *testing.T) {
	var client *MockBank
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.FetchRate(ctx, "USD")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchRate() error = %v, want context.Canceled", err)
	}
}

func TestFailingMockBankReturnsBankError(t *testing.T) {
	client := NewFailingMockBank("Offline Bank", domain.ErrBankUnavailable)

	_, err := client.FetchRate(context.Background(), "USD")
	if !errors.Is(err, domain.ErrBankUnavailable) {
		t.Fatalf("FetchRate() error = %v, want ErrBankUnavailable", err)
	}
	if err.Error() != domain.ErrBankUnavailable.Error() {
		t.Fatalf("FetchRate() error = %q, want %q", err.Error(), domain.ErrBankUnavailable.Error())
	}
}

func TestFailingMockBankWrapsCustomErrorWithBankError(t *testing.T) {
	sourceErr := errors.New("maintenance")
	client := NewFailingMockBank("Offline Bank", sourceErr)

	_, err := client.FetchRate(context.Background(), "USD")
	if !errors.Is(err, domain.ErrBankUnavailable) {
		t.Fatalf("FetchRate() error = %v, want ErrBankUnavailable", err)
	}
	if !errors.Is(err, sourceErr) {
		t.Fatalf("FetchRate() error = %v, want source error", err)
	}
}

func TestMockBankRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := NewMockBank("Test Bank", map[string]domain.CurrencyRate{
		"USD": {Buy: 91.2, Sell: 92.1},
	})

	_, err := client.FetchRate(ctx, "USD")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchRate() error = %v, want context.Canceled", err)
	}
}

func TestDefaultMockBanksContainSuccessfulAndFailingClients(t *testing.T) {
	clients := DefaultMockBanks(time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC))

	if len(clients) != 3 {
		t.Fatalf("len(DefaultMockBanks()) = %d, want 3", len(clients))
	}

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

	if successCount != 2 {
		t.Fatalf("successCount = %d, want 2", successCount)
	}
	if failureCount != 1 {
		t.Fatalf("failureCount = %d, want 1", failureCount)
	}
}

func TestDefaultLiveMockBanksUseFetchTime(t *testing.T) {
	clients := DefaultLiveMockBanks()

	before := time.Now().UTC()
	rate, err := clients[0].FetchRate(context.Background(), "USD")
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("FetchRate() error = %v", err)
	}

	if rate.FetchedAt.IsZero() {
		t.Fatal("FetchedAt is zero, want fetch timestamp")
	}
	if rate.FetchedAt.Before(before) || rate.FetchedAt.After(after) {
		t.Fatalf("FetchedAt = %v, want between %v and %v", rate.FetchedAt, before, after)
	}
}
