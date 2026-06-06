package domain

import "testing"

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
		{name: "non ascii", input: "РУБ", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeCurrency(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("NormalizeCurrency() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeCurrency() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeCurrency() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeCurrencyCodeReturnsDomainType(t *testing.T) {
	code, err := NormalizeCurrencyCode(" usd ")
	if err != nil {
		t.Fatalf("NormalizeCurrencyCode() error = %v", err)
	}
	if code != CurrencyCode("USD") {
		t.Fatalf("NormalizeCurrencyCode() = %q, want USD", code)
	}
	if code.String() != "USD" {
		t.Fatalf("CurrencyCode.String() = %q, want USD", code.String())
	}
}
