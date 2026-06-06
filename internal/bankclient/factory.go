package bankclient

import "fmt"

type SourceOptions struct {
	CBRDailyURL        string
	FrankfurterBaseURL string
	TBankRatesURL      string
	TBankRateCategory  string
}

func NewClients(sources []string, options SourceOptions) ([]BankClient, error) {
	clients := make([]BankClient, 0, len(sources))
	for _, source := range sources {
		switch source {
		case "cbr":
			clients = append(clients, DefaultCBRClients(options.CBRDailyURL)...)
		case "frankfurter":
			clients = append(clients, DefaultFrankfurterClients(options.FrankfurterBaseURL)...)
		case "mock":
			clients = append(clients, DefaultLiveMockBanks()...)
		case "tbank":
			clients = append(clients, DefaultTBankClients(options.TBankRatesURL, options.TBankRateCategory)...)
		default:
			return nil, fmt.Errorf("unknown rate source %q", source)
		}
	}
	if len(clients) == 0 {
		return nil, fmt.Errorf("at least one rate source is required")
	}
	return clients, nil
}
