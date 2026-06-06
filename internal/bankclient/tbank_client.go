package bankclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"currency-rate-aggregator/internal/domain"
)

const (
	DefaultTBankRatesURL      = "https://www.tinkoff.ru/api/v1/currency_rates/"
	DefaultTBankRateCategory  = "DebitCardsTransfers"
	tbankBankName             = "T-Bank"
	tbankTargetCurrency       = "RUB"
	tbankSuccessfulResultCode = "OK"
)

var errTBankMalformedResponse = errors.New("malformed T-Bank response")

type TBankClient struct {
	url      string
	category string
	client   *http.Client
	now      func() time.Time
}

func NewTBankClient(url, category string, client *http.Client) *TBankClient {
	if strings.TrimSpace(url) == "" {
		url = DefaultTBankRatesURL
	}
	if strings.TrimSpace(category) == "" {
		category = DefaultTBankRateCategory
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &TBankClient{
		url:      strings.TrimSpace(url),
		category: strings.TrimSpace(category),
		client:   client,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

func DefaultTBankClients(url, category string) []BankClient {
	return []BankClient{NewTBankClient(url, category, nil)}
}

func (c *TBankClient) Name() string {
	if c == nil {
		return unknownBankName
	}
	return tbankBankName
}

func (c *TBankClient) FetchRate(ctx context.Context, currency string) (domain.CurrencyRate, error) {
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
	if normalized == tbankTargetCurrency {
		return domain.CurrencyRate{
			Currency:  domain.CurrencyCode(tbankTargetCurrency),
			Buy:       1,
			Sell:      1,
			Bank:      tbankBankName,
			FetchedAt: c.now(),
		}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return domain.CurrencyRate{}, fmt.Errorf("%w: build T-Bank request: %w", domain.ErrBankUnavailable, err)
	}
	req.Header.Set("User-Agent", "currency-rate-aggregator/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return domain.CurrencyRate{}, fmt.Errorf("%w: fetch T-Bank rates: %w", domain.ErrBankUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return domain.CurrencyRate{}, fmt.Errorf("%w: T-Bank returned HTTP %d", domain.ErrBankUnavailable, resp.StatusCode)
	}

	var payload tbankRatesResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return domain.CurrencyRate{}, fmt.Errorf("%w: decode JSON: %w", errTBankMalformedResponse, err)
	}
	if payload.ResultCode != "" && !strings.EqualFold(payload.ResultCode, tbankSuccessfulResultCode) {
		return domain.CurrencyRate{}, fmt.Errorf("%w: T-Bank result code %q", domain.ErrBankUnavailable, payload.ResultCode)
	}

	fetchedAt := c.now()
	if payload.Payload.LastUpdate.Milliseconds > 0 {
		fetchedAt = time.UnixMilli(payload.Payload.LastUpdate.Milliseconds).UTC()
	}

	for _, rate := range payload.Payload.Rates {
		if !strings.EqualFold(strings.TrimSpace(rate.Category), c.category) {
			continue
		}
		from, err := domain.NormalizeCurrency(rate.FromCurrency.Name)
		if err != nil || from != normalized {
			continue
		}
		to, err := domain.NormalizeCurrency(rate.ToCurrency.Name)
		if err != nil || to != tbankTargetCurrency {
			continue
		}
		if rate.Buy <= 0 || rate.Sell <= 0 {
			return domain.CurrencyRate{}, fmt.Errorf("%w: invalid buy/sell %v/%v for %s", errTBankMalformedResponse, rate.Buy, rate.Sell, normalized)
		}
		return domain.CurrencyRate{
			Currency:  domain.CurrencyCode(normalized),
			Buy:       rate.Buy,
			Sell:      rate.Sell,
			Bank:      tbankBankName,
			FetchedAt: fetchedAt,
		}, nil
	}

	return domain.CurrencyRate{}, domain.ErrCurrencyNotFound
}

type tbankRatesResponse struct {
	ResultCode string            `json:"resultCode"`
	Payload    tbankRatesPayload `json:"payload"`
}

type tbankRatesPayload struct {
	LastUpdate tbankLastUpdate `json:"lastUpdate"`
	Rates      []tbankRate     `json:"rates"`
}

type tbankLastUpdate struct {
	Milliseconds int64 `json:"milliseconds"`
}

type tbankRate struct {
	Category     string        `json:"category"`
	FromCurrency tbankCurrency `json:"fromCurrency"`
	ToCurrency   tbankCurrency `json:"toCurrency"`
	Buy          float64       `json:"buy"`
	Sell         float64       `json:"sell"`
}

type tbankCurrency struct {
	Name string `json:"name"`
}
