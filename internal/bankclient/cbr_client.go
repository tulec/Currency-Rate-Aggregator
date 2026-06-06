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
	defer resp.Body.Close()

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
		return windows1251ToUTF8(input)
	case "utf-8", "utf8":
		return input, nil
	default:
		return nil, fmt.Errorf("unsupported charset %q", label)
	}
}

func windows1251ToUTF8(input io.Reader) (io.Reader, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}

	var out strings.Builder
	out.Grow(len(data))
	for _, b := range data {
		out.WriteRune(windows1251Rune(b))
	}
	return strings.NewReader(out.String()), nil
}

func windows1251Rune(b byte) rune {
	if b < 0x80 {
		return rune(b)
	}
	if b >= 0xC0 {
		return rune(0x0410 + int(b) - 0xC0)
	}

	table := [...]rune{
		0x0402, 0x0403, 0x201A, 0x0453, 0x201E, 0x2026, 0x2020, 0x2021,
		0x20AC, 0x2030, 0x0409, 0x2039, 0x040A, 0x040C, 0x040B, 0x040F,
		0x0452, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
		0x0098, 0x2122, 0x0459, 0x203A, 0x045A, 0x045C, 0x045B, 0x045F,
		0x00A0, 0x040E, 0x045E, 0x0408, 0x00A4, 0x0490, 0x00A6, 0x00A7,
		0x0401, 0x00A9, 0x0404, 0x00AB, 0x00AC, 0x00AD, 0x00AE, 0x0407,
		0x00B0, 0x00B1, 0x0406, 0x0456, 0x0491, 0x00B5, 0x00B6, 0x00B7,
		0x0451, 0x2116, 0x0454, 0x00BB, 0x0458, 0x0405, 0x0455, 0x0457,
	}
	return table[b-0x80]
}
