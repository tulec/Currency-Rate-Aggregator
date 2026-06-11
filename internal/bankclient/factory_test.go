package bankclient

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNewClientsBuildsConfiguredSources(t *testing.T) {
	clients, err := NewClients([]string{"cbr", "mock"}, SourceOptions{
		CBRDailyURL: "https://example.test/cbr.xml",
	})
	require.NoErrorf(t, err,
		"NewClients() error = %v", err)
	require.Lenf(t, clients, 4,
		"clients = %d, want 4", len(clients))
	require.EqualValuesf(t, cbrBankName, clients[0].Name(),
		"first client = %q, want %s", clients[0].Name(), cbrBankName)

}

func TestNewClientsBuildsFrankfurterSource(t *testing.T) {
	clients, err := NewClients([]string{"frankfurter"}, SourceOptions{
		FrankfurterBaseURL: "https://example.test/frankfurter",
	})
	require.NoErrorf(t, err,
		"NewClients() error = %v", err)
	require.Lenf(t, clients, 1,
		"clients = %d, want 1", len(clients))
	require.EqualValuesf(t, frankfurterBankName, clients[0].Name(),
		"client = %q, want %s", clients[0].Name(), frankfurterBankName)

}

func TestNewClientsBuildsTBankSource(t *testing.T) {
	clients, err := NewClients([]string{"tbank"}, SourceOptions{
		TBankRatesURL:     "https://example.test/tbank/rates",
		TBankRateCategory: "DebitCardsTransfers",
	})
	require.NoErrorf(t, err,
		"NewClients() error = %v", err)
	require.Lenf(t, clients, 1,
		"clients = %d, want 1", len(clients))
	require.EqualValuesf(t, tbankBankName, clients[0].Name(),
		"client = %q, want %s", clients[0].Name(), tbankBankName)

}

func TestNewClientsRejectsUnknownSource(t *testing.T) {
	if _, err := NewClients([]string{"unknown"}, SourceOptions{}); err == nil {
		require.FailNow(t, "test failed", "NewClients() error = nil, want error")
	}
}

func TestNewClientsRequiresSources(t *testing.T) {
	if _, err := NewClients(nil, SourceOptions{}); err == nil {
		require.FailNow(t, "test failed", "NewClients() error = nil, want error")
	}
}
