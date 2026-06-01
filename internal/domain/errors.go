package domain

import "errors"

var (
	ErrBankUnavailable     = errors.New("bank unavailable")
	ErrCurrencyNotFound    = errors.New("currency not found")
	ErrInvalidCurrencyCode = errors.New("invalid currency code")
)
