package bankclient

import (
	"context"
	"errors"
	"time"

	"currency-rate-aggregator/internal/domain"
)

type MockBank struct {
	name  string
	rates map[string]domain.CurrencyRate
	err   error
}

const unknownBankName = "unknown"

func NewMockBank(name string, rates map[string]domain.CurrencyRate) *MockBank {
	copied := make(map[string]domain.CurrencyRate, len(rates))
	for currency, rate := range rates {
		normalized, err := domain.NormalizeCurrency(currency)
		if err != nil {
			continue
		}
		rate.Currency = normalized
		rate.Bank = name
		copied[normalized] = rate
	}

	return &MockBank{
		name:  name,
		rates: copied,
	}
}

func NewFailingMockBank(name string, err error) *MockBank {
	if err == nil {
		err = domain.ErrBankUnavailable
	}

	return &MockBank{
		name: name,
		err:  err,
	}
}

func DefaultMockBanks(now time.Time) []BankClient {
	return []BankClient{
		NewMockBank("North Bank", map[string]domain.CurrencyRate{
			"USD": {Buy: 91.20, Sell: 92.10, FetchedAt: now},
			"EUR": {Buy: 99.40, Sell: 100.30, FetchedAt: now},
		}),
		NewMockBank("Metro Bank", map[string]domain.CurrencyRate{
			"USD": {Buy: 91.05, Sell: 91.80, FetchedAt: now},
			"EUR": {Buy: 99.10, Sell: 100.05, FetchedAt: now},
		}),
		NewFailingMockBank("Offline Bank", domain.ErrBankUnavailable),
	}
}

func DefaultLiveMockBanks() []BankClient {
	return DefaultMockBanks(time.Time{})
}

func (b *MockBank) Name() string {
	if b == nil {
		return unknownBankName
	}
	return b.name
}

func (b *MockBank) FetchRate(ctx context.Context, currency string) (domain.CurrencyRate, error) {
	if err := ctx.Err(); err != nil {
		return domain.CurrencyRate{}, err
	}
	if b == nil {
		return domain.CurrencyRate{}, domain.ErrBankUnavailable
	}

	if b.err != nil {
		if errors.Is(b.err, domain.ErrBankUnavailable) {
			return domain.CurrencyRate{}, b.err
		}
		return domain.CurrencyRate{}, errors.Join(domain.ErrBankUnavailable, b.err)
	}

	normalized, err := domain.NormalizeCurrency(currency)
	if err != nil {
		return domain.CurrencyRate{}, err
	}

	rate, ok := b.rates[normalized]
	if !ok {
		return domain.CurrencyRate{}, domain.ErrCurrencyNotFound
	}

	rate.Currency = normalized
	rate.Bank = b.name
	if rate.FetchedAt.IsZero() {
		rate.FetchedAt = time.Now().UTC()
	} else {
		rate.FetchedAt = rate.FetchedAt.UTC()
	}

	return rate, nil
}
