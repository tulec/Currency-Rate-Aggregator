package bankclient

import (
	"context"

	"currency-rate-aggregator/internal/domain"
)

// BankClient is the adapter boundary for every external rate source.
//
// Implementations must:
//   - respect context cancellation and deadlines;
//   - normalize successful responses into domain.CurrencyRate;
//   - return domain.ErrCurrencyNotFound when the source has no requested currency;
//   - wrap temporary source/network failures with domain.ErrBankUnavailable;
//   - keep source-specific HTTP, JSON, XML, or HTML parsing inside the client.
type BankClient interface {
	// Name returns a stable human-readable source name used in API responses,
	// logs, metrics, and persisted history rows.
	Name() string

	// FetchRate returns the source's rate for one normalized or normalizable
	// three-letter currency code. The rate is expected to be expressed against
	// RUB, where Buy means the source buys the foreign currency for RUB and Sell
	// means the source sells the foreign currency for RUB.
	FetchRate(ctx context.Context, currency string) (domain.CurrencyRate, error)
}
