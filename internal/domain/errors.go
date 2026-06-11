package domain

import (
	"errors"
	"fmt"
)

var (
	ErrBankUnavailable     = errors.New("bank unavailable")
	ErrCurrencyNotFound    = errors.New("currency not found")
	ErrInvalidCurrencyCode = errors.New("invalid currency code")
)

type BankUnavailableError struct {
	Bank      string
	Operation string
	Err       error
}

func (e *BankUnavailableError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf(
			"%s: %s: %s",
			e.Bank,
			e.Operation,
			ErrBankUnavailable,
		)
	}
	return fmt.Sprintf(
		"%s: %s: %s: %v",
		e.Bank,
		e.Operation,
		ErrBankUnavailable,
		e.Err,
	)
}

func (e *BankUnavailableError) Unwrap() error {
	return e.Err
}

func (e *BankUnavailableError) Is(target error) bool {
	return errors.Is(target, ErrBankUnavailable)
}
