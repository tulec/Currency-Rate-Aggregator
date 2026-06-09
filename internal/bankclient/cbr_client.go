package bankclient

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"currency-rate-aggregator/internal/domain"
	"golang.org/x/text/encoding/charmap"
)

const (
	DefaultCBRDailyURL = "https://www.cbr.ru/scripts/XML_daily.asp"
	cbrBankName        = "Bank of Russia"
)

var errCBRMalformedResponse = errors.New("malformed CBR response")

type CBRClient struct {
	url    string
	client *http.Client
	now    func() time.Time
}

func NewCBRClient(url string, client *http.Client) *CBRClient {
	if strings.TrimSpace(url) == "" {
		url = DefaultCBRDailyURL
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &CBRClient{
		url:    url,
		client: client,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

func DefaultCBRClients(url string) []BankClient {
	return []BankClient{NewCBRClient(url, nil)}
}

func (c *CBRClient) Name() string {
	if c == nil {
		return unknownBankName
	}
	return cbrBankName
}

func (c *CBRClient) FetchRate(ctx context.Context, currency string) (domain.CurrencyRate, error) {
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
			Bank:      cbrBankName,
			FetchedAt: c.now(),
		}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return domain.CurrencyRate{}, fmt.Errorf("%w: build CBR request: %w", domain.ErrBankUnavailable, err)
	}
	req.Header.Set("User-Agent", "currency-rate-aggregator/1.0")

	resp, err := c.client.Do(req)
	if err != nil {
		return domain.CurrencyRate{}, fmt.Errorf("%w: fetch CBR rates: %w", domain.ErrBankUnavailable, err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
		}
	}(resp.Body)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return domain.CurrencyRate{}, fmt.Errorf("%w: CBR returned HTTP %d", domain.ErrBankUnavailable, resp.StatusCode)
	}

	var payload cbrDailyRates
	decoder := xml.NewDecoder(resp.Body)
	decoder.CharsetReader = cbrCharsetReader
	if err := decoder.Decode(&payload); err != nil {
		return domain.CurrencyRate{}, fmt.Errorf("%w: decode XML: %w", errCBRMalformedResponse, err)
	}

	fetchedAt := c.now()
	if payload.Date != "" {
		parsed, err := time.ParseInLocation("02.01.2006", payload.Date, time.UTC)
		if err != nil {
			return domain.CurrencyRate{}, fmt.Errorf("%w: parse CBR date %q: %w", errCBRMalformedResponse, payload.Date, err)
		}
		fetchedAt = parsed.UTC()
	}

	for _, valute := range payload.Valutes {
		if strings.EqualFold(strings.TrimSpace(valute.CharCode), normalized) {
			rate, err := valute.rate()
			if err != nil {
				return domain.CurrencyRate{}, err
			}
			return domain.CurrencyRate{
				Currency:  domain.CurrencyCode(normalized),
				Buy:       rate,
				Sell:      rate,
				Bank:      cbrBankName,
				FetchedAt: fetchedAt,
			}, nil
		}
	}

	return domain.CurrencyRate{}, domain.ErrCurrencyNotFound
}

type cbrDailyRates struct {
	Date    string      `xml:"Date,attr"`
	Valutes []cbrValute `xml:"Valute"`
}

type cbrValute struct {
	CharCode  string `xml:"CharCode"`
	Nominal   int    `xml:"Nominal"`
	Value     string `xml:"Value"`
	VunitRate string `xml:"VunitRate"`
}

func (v cbrValute) rate() (float64, error) {
	if strings.TrimSpace(v.VunitRate) != "" {
		return parseCBRDecimal(v.VunitRate)
	}

	value, err := parseCBRDecimal(v.Value)
	if err != nil {
		return 0, err
	}
	if v.Nominal <= 0 {
		return 0, fmt.Errorf("%w: invalid nominal %d for %s", errCBRMalformedResponse, v.Nominal, v.CharCode)
	}
	return value / float64(v.Nominal), nil
}

func parseCBRDecimal(value string) (float64, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(value), ",", ".")
	parsed, err := strconv.ParseFloat(normalized, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid decimal %q: %w", errCBRMalformedResponse, value, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%w: invalid decimal %q", errCBRMalformedResponse, value)
	}
	return parsed, nil
}

func cbrCharsetReader(label string, input io.Reader) (io.Reader, error) {
	normalized := strings.ToLower(strings.TrimSpace(label))
	switch normalized {
	case "windows-1251", "cp1251":
		return charmap.Windows1251.NewDecoder().Reader(input), nil
	case "utf-8", "utf8":
		return input, nil
	default:
		return nil, fmt.Errorf("unsupported charset %q", label)
	}
}
