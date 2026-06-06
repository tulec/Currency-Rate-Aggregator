package bankclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"currency-rate-aggregator/internal/domain"
)

const (
	DefaultFrankfurterBaseURL = "https://api.frankfurter.dev/v2"
	frankfurterBankName       = "Frankfurter"
)

var errFrankfurterMalformedResponse = errors.New("malformed Frankfurter response")

type FrankfurterClient struct {
	baseURL string
	client  *http.Client
	now     func() time.Time
}

func NewFrankfurterClient(baseURL string, client *http.Client) *FrankfurterClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultFrankfurterBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &FrankfurterClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  client,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

func DefaultFrankfurterClients(baseURL string) []BankClient {
	return []BankClient{NewFrankfurterClient(baseURL, nil)}
}

func (c *FrankfurterClient) Name() string {
	if c == nil {
		return unknownBankName
	}
	return frankfurterBankName
}

func (c *FrankfurterClient) FetchRate(ctx context.Context, currency string) (domain.CurrencyRate, error) {
	if err := ctx.Err(); err != nil {
		return domain.CurrencyRate{}, err
	}
	if c == nil || c.client == nil {
		return domain.CurrencyRate{}, domain.ErrBankUnavailable
	}

	normalized, err := domain.NormalizeCurrency(currency)
	if err != nil {
		return domain.CurrencyRate{}, err
	}
	if normalized == "RUB" {
		return domain.CurrencyRate{
			Currency:  "RUB",
			Buy:       1,
			Sell:      1,
			Bank:      frankfurterBankName,
			FetchedAt: c.now(),
		}, nil
	}

	endpoint, err := frankfurterRateURL(c.baseURL, normalized, "RUB")
	if err != nil {
		return domain.CurrencyRate{}, fmt.Errorf("%w: build Frankfurter request: %w", domain.ErrBankUnavailable, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return domain.CurrencyRate{}, fmt.Errorf("%w: build Frankfurter request: %w", domain.ErrBankUnavailable, err)
	}
	req.Header.Set("User-Agent", "currency-rate-aggregator/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return domain.CurrencyRate{}, fmt.Errorf("%w: fetch Frankfurter rates: %w", domain.ErrBankUnavailable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnprocessableEntity:
		return domain.CurrencyRate{}, domain.ErrCurrencyNotFound
	case resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices:
		return domain.CurrencyRate{}, fmt.Errorf("%w: Frankfurter returned HTTP %d", domain.ErrBankUnavailable, resp.StatusCode)
	}

	var payload frankfurterRateResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return domain.CurrencyRate{}, fmt.Errorf("%w: decode JSON: %w", errFrankfurterMalformedResponse, err)
	}

	if payload.Rate <= 0 {
		return domain.CurrencyRate{}, fmt.Errorf("%w: invalid rate %v", errFrankfurterMalformedResponse, payload.Rate)
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Base), normalized) {
		return domain.CurrencyRate{}, fmt.Errorf("%w: base %q does not match %s", errFrankfurterMalformedResponse, payload.Base, normalized)
	}
	if !strings.EqualFold(strings.TrimSpace(payload.Quote), "RUB") {
		return domain.CurrencyRate{}, fmt.Errorf("%w: quote %q does not match RUB", errFrankfurterMalformedResponse, payload.Quote)
	}

	fetchedAt := c.now()
	if payload.Date != "" {
		parsed, err := time.ParseInLocation("2006-01-02", payload.Date, time.UTC)
		if err != nil {
			return domain.CurrencyRate{}, fmt.Errorf("%w: parse date %q: %w", errFrankfurterMalformedResponse, payload.Date, err)
		}
		fetchedAt = parsed.UTC()
	}

	return domain.CurrencyRate{
		Currency:  domain.CurrencyCode(normalized),
		Buy:       payload.Rate,
		Sell:      payload.Rate,
		Bank:      frankfurterBankName,
		FetchedAt: fetchedAt,
	}, nil
}

type frankfurterRateResponse struct {
	Base  string  `json:"base"`
	Quote string  `json:"quote"`
	Date  string  `json:"date"`
	Rate  float64 `json:"rate"`
}

func frankfurterRateURL(baseURL, baseCurrency, quoteCurrency string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid base URL %q", baseURL)
	}
	return url.JoinPath(parsed.String(), "rate", baseCurrency, quoteCurrency)
}
