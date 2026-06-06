package bankclient

import "testing"

func TestNewClientsBuildsConfiguredSources(t *testing.T) {
	clients, err := NewClients([]string{"cbr", "mock"}, SourceOptions{
		CBRDailyURL: "https://example.test/cbr.xml",
	})
	if err != nil {
		t.Fatalf("NewClients() error = %v", err)
	}

	if len(clients) != 4 {
		t.Fatalf("clients = %d, want 4", len(clients))
	}
	if clients[0].Name() != cbrBankName {
		t.Fatalf("first client = %q, want %s", clients[0].Name(), cbrBankName)
	}
}

func TestNewClientsBuildsFrankfurterSource(t *testing.T) {
	clients, err := NewClients([]string{"frankfurter"}, SourceOptions{
		FrankfurterBaseURL: "https://example.test/frankfurter",
	})
	if err != nil {
		t.Fatalf("NewClients() error = %v", err)
	}

	if len(clients) != 1 {
		t.Fatalf("clients = %d, want 1", len(clients))
	}
	if clients[0].Name() != frankfurterBankName {
		t.Fatalf("client = %q, want %s", clients[0].Name(), frankfurterBankName)
	}
}

func TestNewClientsBuildsTBankSource(t *testing.T) {
	clients, err := NewClients([]string{"tbank"}, SourceOptions{
		TBankRatesURL:     "https://example.test/tbank/rates",
		TBankRateCategory: "DebitCardsTransfers",
	})
	if err != nil {
		t.Fatalf("NewClients() error = %v", err)
	}

	if len(clients) != 1 {
		t.Fatalf("clients = %d, want 1", len(clients))
	}
	if clients[0].Name() != tbankBankName {
		t.Fatalf("client = %q, want %s", clients[0].Name(), tbankBankName)
	}
}

func TestNewClientsRejectsUnknownSource(t *testing.T) {
	if _, err := NewClients([]string{"unknown"}, SourceOptions{}); err == nil {
		t.Fatal("NewClients() error = nil, want error")
	}
}

func TestNewClientsRequiresSources(t *testing.T) {
	if _, err := NewClients(nil, SourceOptions{}); err == nil {
		t.Fatal("NewClients() error = nil, want error")
	}
}
