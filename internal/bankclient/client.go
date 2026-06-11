package bankclient

import (
	"context"

	"currency-rate-aggregator/internal/domain"
)

type BankClient interface {
	Name() string
	FetchRate(ctx context.Context, currency string) (domain.CurrencyRate, error)
}
