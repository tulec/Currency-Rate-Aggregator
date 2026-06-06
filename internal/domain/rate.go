package domain

import (
	"strings"
	"time"
	"unicode"
)

type CurrencyCode string

func (c CurrencyCode) String() string {
	return string(c)
}

type CurrencyRate struct {
	Currency  CurrencyCode `json:"currency"`
	Buy       float64      `json:"buy"`
	Sell      float64      `json:"sell"`
	Bank      string       `json:"bank"`
	FetchedAt time.Time    `json:"fetched_at"`
}

type RateResult struct {
	Currency  CurrencyCode   `json:"currency"`
	BestBuy   CurrencyRate   `json:"best_buy"`
	BestSell  CurrencyRate   `json:"best_sell"`
	Sources   []CurrencyRate `json:"sources"`
	UpdatedAt time.Time      `json:"updated_at"`
}

func NormalizeCurrency(currency string) (string, error) {
	code, err := NormalizeCurrencyCode(currency)
	if err != nil {
		return "", err
	}
	return code.String(), nil
}

func NormalizeCurrencyCode(currency string) (CurrencyCode, error) {
	normalized := strings.ToUpper(strings.TrimSpace(currency))
	if len(normalized) != 3 {
		return "", ErrInvalidCurrencyCode
	}

	for _, r := range normalized {
		if !unicode.IsLetter(r) || r > unicode.MaxASCII {
			return "", ErrInvalidCurrencyCode
		}
	}

	return CurrencyCode(normalized), nil
}
