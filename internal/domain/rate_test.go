package domain

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNormalizeCurrency(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "upper case", input: "USD", want: "USD"},
		{name: "lower case with spaces", input: " eur ", want: "EUR"},
		{name: "too short", input: "US", wantErr: true},
		{name: "too long", input: "USDT", wantErr: true},
		{name: "digits", input: "RU1", wantErr: true},
		{name: "non ascii", input: "Р РЈР‘", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeCurrency(tt.input)
			if tt.wantErr {
				require.Error(t, err,
					"NormalizeCurrency() error = nil, want error")

				return
			}
			require.NoErrorf(t, err,
				"NormalizeCurrency() error = %v", err)
			require.EqualValuesf(t, tt.want, got,
				"NormalizeCurrency() = %q, want %q", got, tt.want)

		})
	}
}

func TestNormalizeCurrencyCodeReturnsDomainType(t *testing.T) {
	code, err := NormalizeCurrencyCode(" usd ")
	require.NoErrorf(t, err,
		"NormalizeCurrencyCode() error = %v", err)
	require.EqualValuesf(t, CurrencyCode("USD"), code,
		"NormalizeCurrencyCode() = %q, want USD", code)
	require.EqualValuesf(t, "USD", code.String(),
		"CurrencyCode.String() = %q, want USD", code.String())

}
