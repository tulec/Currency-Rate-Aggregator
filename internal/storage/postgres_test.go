package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"currency-rate-aggregator/internal/domain"
)

func TestPostgresStoreMigrateCreatesTableAndIndex(t *testing.T) {
	db := &fakeDB{}
	store := &PostgresStore{db: db}

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	if len(db.execs) != 2 {
		t.Fatalf("executed statements = %d, want 2", len(db.execs))
	}
	if !strings.Contains(db.execs[0].query, "BIGSERIAL PRIMARY KEY") {
		t.Fatalf("first migration query = %q, want postgres id", db.execs[0].query)
	}
	if !strings.Contains(db.execs[0].query, "TIMESTAMPTZ NOT NULL") {
		t.Fatalf("first migration query = %q, want timestamptz fetched_at", db.execs[0].query)
	}
	if !strings.Contains(db.execs[1].query, "CREATE INDEX IF NOT EXISTS idx_rates_currency_fetched_at") {
		t.Fatalf("second migration query = %q, want rates index", db.execs[1].query)
	}
	if !strings.Contains(db.execs[1].query, "ON rates(currency, fetched_at DESC, id DESC)") {
		t.Fatalf("second migration query = %q, want history query order covered", db.execs[1].query)
	}
}

func TestPostgresStoreReportsUnconfiguredStore(t *testing.T) {
	var nilStore *PostgresStore
	if err := nilStore.Migrate(context.Background()); !errors.Is(err, ErrStoreNotConfigured) {
		t.Fatalf("nil Migrate() error = %v, want ErrStoreNotConfigured", err)
	}

	store := NewPostgresStore(nil)
	if err := store.Migrate(context.Background()); !errors.Is(err, ErrStoreNotConfigured) {
		t.Fatalf("Migrate() error = %v, want ErrStoreNotConfigured", err)
	}
	if err := store.SaveRates(context.Background(), []domain.CurrencyRate{{Currency: "USD", Buy: 91.2, Sell: 92.1, Bank: "Bank A", FetchedAt: time.Now()}}); !errors.Is(err, ErrStoreNotConfigured) {
		t.Fatalf("SaveRates() error = %v, want ErrStoreNotConfigured", err)
	}
	if err := store.SaveRates(context.Background(), nil); !errors.Is(err, ErrStoreNotConfigured) {
		t.Fatalf("SaveRates() with empty rates error = %v, want ErrStoreNotConfigured", err)
	}
	if _, err := store.History(context.Background(), "USD", 10); !errors.Is(err, ErrStoreNotConfigured) {
		t.Fatalf("History() error = %v, want ErrStoreNotConfigured", err)
	}
	if _, err := store.HistoryByDate(context.Background(), "USD", time.Now(), time.Now().Add(time.Hour), 10); !errors.Is(err, ErrStoreNotConfigured) {
		t.Fatalf("HistoryByDate() error = %v, want ErrStoreNotConfigured", err)
	}
}

func TestPostgresStoreSaveRatesInsertsRatesTransactionally(t *testing.T) {
	tx := &fakeTx{}
	db := &fakeDB{tx: tx}
	store := &PostgresStore{db: db}
	fetchedAt := time.Date(2026, 5, 18, 10, 0, 0, 123, time.FixedZone("MSK", 3*60*60))

	err := store.SaveRates(context.Background(), []domain.CurrencyRate{
		{Currency: " usd ", Buy: 91.2, Sell: 92.1, Bank: "Bank A", FetchedAt: fetchedAt},
	})
	if err != nil {
		t.Fatalf("SaveRates() error = %v", err)
	}

	if !tx.committed {
		t.Fatal("transaction was not committed")
	}
	if len(tx.execs) != 1 {
		t.Fatalf("insert statements = %d, want 1", len(tx.execs))
	}
	if !strings.Contains(tx.execs[0].query, "VALUES ($1, $2, $3, $4, $5)") {
		t.Fatalf("insert query = %q, want postgres placeholders", tx.execs[0].query)
	}
	if tx.execs[0].args[0] != "USD" {
		t.Fatalf("stored currency = %v, want USD", tx.execs[0].args[0])
	}
	if tx.execs[0].args[4] != fetchedAt.UTC() {
		t.Fatalf("stored fetched_at = %v, want UTC time", tx.execs[0].args[4])
	}
}

func TestPostgresStoreSaveRatesRollsBackOnInsertError(t *testing.T) {
	insertErr := errors.New("insert failed")
	tx := &fakeTx{execErr: insertErr}
	db := &fakeDB{tx: tx}
	store := &PostgresStore{db: db}

	err := store.SaveRates(context.Background(), []domain.CurrencyRate{
		{Currency: "USD", Buy: 91.2, Sell: 92.1, Bank: "Bank A", FetchedAt: time.Now()},
	})
	if !errors.Is(err, insertErr) {
		t.Fatalf("SaveRates() error = %v, want insert error", err)
	}
	if !tx.rolledBack {
		t.Fatal("transaction was not rolled back")
	}
	if tx.committed {
		t.Fatal("transaction was committed after insert error")
	}
}

func TestPostgresStoreSaveRatesReportsRollbackError(t *testing.T) {
	insertErr := errors.New("insert failed")
	rollbackErr := errors.New("rollback failed")
	tx := &fakeTx{execErr: insertErr, rollbackErr: rollbackErr}
	store := &PostgresStore{db: &fakeDB{tx: tx}}

	err := store.SaveRates(context.Background(), []domain.CurrencyRate{
		{Currency: "USD", Buy: 91.2, Sell: 92.1, Bank: "Bank A", FetchedAt: time.Now()},
	})
	if !errors.Is(err, insertErr) {
		t.Fatalf("SaveRates() error = %v, want insert error", err)
	}
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("SaveRates() error = %v, want rollback error", err)
	}
	if !strings.Contains(err.Error(), "rollback save rates transaction") {
		t.Fatalf("SaveRates() error = %q, want rollback context", err)
	}
	if !tx.rolledBack {
		t.Fatal("transaction was not rolled back")
	}
	if tx.committed {
		t.Fatal("transaction was committed after insert error")
	}
}

func TestPostgresStoreSaveRatesReportsBeginError(t *testing.T) {
	beginErr := errors.New("begin failed")
	store := &PostgresStore{db: &fakeDB{beginErr: beginErr}}

	err := store.SaveRates(context.Background(), []domain.CurrencyRate{
		{Currency: "USD", Buy: 91.2, Sell: 92.1, Bank: "Bank A", FetchedAt: time.Now()},
	})
	if !errors.Is(err, beginErr) {
		t.Fatalf("SaveRates() error = %v, want begin error", err)
	}
	if !strings.Contains(err.Error(), "begin save rates transaction") {
		t.Fatalf("SaveRates() error = %q, want begin context", err)
	}
}

func TestPostgresStoreSaveRatesReportsCommitError(t *testing.T) {
	commitErr := errors.New("commit failed")
	tx := &fakeTx{commitErr: commitErr}
	store := &PostgresStore{db: &fakeDB{tx: tx}}

	err := store.SaveRates(context.Background(), []domain.CurrencyRate{
		{Currency: "USD", Buy: 91.2, Sell: 92.1, Bank: "Bank A", FetchedAt: time.Now()},
	})
	if !errors.Is(err, commitErr) {
		t.Fatalf("SaveRates() error = %v, want commit error", err)
	}
	if !strings.Contains(err.Error(), "commit save rates transaction") {
		t.Fatalf("SaveRates() error = %q, want commit context", err)
	}
	if !tx.rolledBack {
		t.Fatal("transaction was not rolled back after commit error")
	}
}

func TestPostgresStoreSaveRatesReportsInvalidCurrencyContext(t *testing.T) {
	tx := &fakeTx{}
	store := &PostgresStore{db: &fakeDB{tx: tx}}

	err := store.SaveRates(context.Background(), []domain.CurrencyRate{
		{Currency: "USDT", Buy: 91.2, Sell: 92.1, Bank: "Bank A", FetchedAt: time.Now()},
	})
	if !errors.Is(err, domain.ErrInvalidCurrencyCode) {
		t.Fatalf("SaveRates() error = %v, want ErrInvalidCurrencyCode", err)
	}
	if !strings.Contains(err.Error(), `normalize rate currency "USDT" from Bank A`) {
		t.Fatalf("SaveRates() error = %q, want currency and bank context", err)
	}
	if !tx.rolledBack {
		t.Fatal("transaction was not rolled back")
	}
	if tx.committed {
		t.Fatal("transaction was committed after invalid currency")
	}
	if len(tx.execs) != 0 {
		t.Fatalf("insert statements = %d, want 0", len(tx.execs))
	}
}

func TestPostgresStoreHistoryNormalizesCurrencyAndCapsLimit(t *testing.T) {
	fetchedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.FixedZone("MSK", 3*60*60))
	rows := &fakeRows{
		values: [][]any{
			{"USD", 91.2, 92.1, "Bank A", fetchedAt},
		},
	}
	db := &fakeDB{rows: rows}
	store := &PostgresStore{db: db}

	rates, err := store.History(context.Background(), " usd ", MaxHistoryLimit+1)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}

	if len(rates) != 1 {
		t.Fatalf("rates = %d, want 1", len(rates))
	}
	if rates[0].FetchedAt != fetchedAt.UTC() {
		t.Fatalf("FetchedAt = %v, want %v", rates[0].FetchedAt, fetchedAt.UTC())
	}
	if db.query.args[0] != "USD" {
		t.Fatalf("query currency = %v, want USD", db.query.args[0])
	}
	if db.query.args[1] != MaxHistoryLimit {
		t.Fatalf("query limit = %v, want %d", db.query.args[1], MaxHistoryLimit)
	}
	if !strings.Contains(db.query.query, "WHERE currency = $1") {
		t.Fatalf("history query = %q, want postgres placeholders", db.query.query)
	}
	if !rows.closed {
		t.Fatal("rows were not closed")
	}
}

func TestPostgresStoreHistoryReturnsEmptySliceWhenNoRows(t *testing.T) {
	store := &PostgresStore{db: &fakeDB{}}

	rates, err := store.History(context.Background(), "USD", 10)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}

	if rates == nil {
		t.Fatal("History() rates is nil, want empty slice")
	}
	if len(rates) != 0 {
		t.Fatalf("rates = %d, want 0", len(rates))
	}
}

func TestPostgresStoreHistoryByDateFiltersRangeAndCapsLimit(t *testing.T) {
	from := time.Date(2026, 6, 1, 3, 0, 0, 0, time.FixedZone("MSK", 3*60*60))
	to := time.Date(2026, 6, 5, 3, 0, 0, 0, time.FixedZone("MSK", 3*60*60))
	fetchedAt := time.Date(2026, 6, 2, 10, 0, 0, 0, time.FixedZone("MSK", 3*60*60))
	rows := &fakeRows{
		values: [][]any{
			{"USD", 91.2, 92.1, "Bank A", fetchedAt},
		},
	}
	db := &fakeDB{rows: rows}
	store := &PostgresStore{db: db}

	rates, err := store.HistoryByDate(context.Background(), " usd ", from, to, MaxHistoryLimit+1)
	if err != nil {
		t.Fatalf("HistoryByDate() error = %v", err)
	}

	if len(rates) != 1 {
		t.Fatalf("rates = %d, want 1", len(rates))
	}
	if rates[0].FetchedAt != fetchedAt.UTC() {
		t.Fatalf("FetchedAt = %v, want %v", rates[0].FetchedAt, fetchedAt.UTC())
	}
	if db.query.args[0] != "USD" {
		t.Fatalf("query currency = %v, want USD", db.query.args[0])
	}
	if db.query.args[1] != from.UTC() {
		t.Fatalf("query from = %v, want %v", db.query.args[1], from.UTC())
	}
	if db.query.args[2] != to.UTC() {
		t.Fatalf("query to = %v, want %v", db.query.args[2], to.UTC())
	}
	if db.query.args[3] != MaxHistoryLimit {
		t.Fatalf("query limit = %v, want %d", db.query.args[3], MaxHistoryLimit)
	}
	if !strings.Contains(db.query.query, "fetched_at >= $2") || !strings.Contains(db.query.query, "fetched_at < $3") {
		t.Fatalf("history by date query = %q, want date range filter", db.query.query)
	}
	if !rows.closed {
		t.Fatal("rows were not closed")
	}
}

func TestPostgresStoreHistoryByDateRejectsInvalidCurrency(t *testing.T) {
	store := &PostgresStore{db: &fakeDB{}}

	_, err := store.HistoryByDate(context.Background(), "USDT", time.Now(), time.Now().Add(time.Hour), 10)
	if !errors.Is(err, domain.ErrInvalidCurrencyCode) {
		t.Fatalf("HistoryByDate() error = %v, want ErrInvalidCurrencyCode", err)
	}
	if !strings.Contains(err.Error(), `normalize history currency "USDT"`) {
		t.Fatalf("HistoryByDate() error = %q, want currency context", err)
	}
}

func TestPostgresStoreHistoryRejectsInvalidCurrency(t *testing.T) {
	store := &PostgresStore{db: &fakeDB{}}

	_, err := store.History(context.Background(), "USDT", 10)
	if !errors.Is(err, domain.ErrInvalidCurrencyCode) {
		t.Fatalf("History() error = %v, want ErrInvalidCurrencyCode", err)
	}
	if !strings.Contains(err.Error(), `normalize history currency "USDT"`) {
		t.Fatalf("History() error = %q, want currency context", err)
	}
}

func TestPostgresStoreHistoryReportsQueryError(t *testing.T) {
	queryErr := errors.New("query failed")
	store := &PostgresStore{db: &fakeDB{queryErr: queryErr}}

	_, err := store.History(context.Background(), "USD", 10)
	if !errors.Is(err, queryErr) {
		t.Fatalf("History() error = %v, want query error", err)
	}
	if !strings.Contains(err.Error(), "query rate history") {
		t.Fatalf("History() error = %q, want query context", err)
	}
}

func TestPostgresStoreHistoryReportsScanError(t *testing.T) {
	scanErr := errors.New("scan failed")
	rows := &fakeRows{
		values:  [][]any{{"USD", 91.2, 92.1, "Bank A", time.Now()}},
		scanErr: scanErr,
	}
	store := &PostgresStore{db: &fakeDB{rows: rows}}

	_, err := store.History(context.Background(), "USD", 10)
	if !errors.Is(err, scanErr) {
		t.Fatalf("History() error = %v, want scan error", err)
	}
	if !strings.Contains(err.Error(), "scan rate history row") {
		t.Fatalf("History() error = %q, want scan context", err)
	}
	if !rows.closed {
		t.Fatal("rows were not closed after scan error")
	}
}

func TestPostgresStoreHistoryReportsRowsError(t *testing.T) {
	rowsErr := errors.New("rows failed")
	rows := &fakeRows{err: rowsErr}
	store := &PostgresStore{db: &fakeDB{rows: rows}}

	_, err := store.History(context.Background(), "USD", 10)
	if !errors.Is(err, rowsErr) {
		t.Fatalf("History() error = %v, want rows error", err)
	}
	if !strings.Contains(err.Error(), "read rate history rows") {
		t.Fatalf("History() error = %q, want rows context", err)
	}
	if !rows.closed {
		t.Fatal("rows were not closed after rows error")
	}
}

func TestPostgresStoreHistoryReportsRowsCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	rows := &fakeRows{
		values:   [][]any{{"USD", 91.2, 92.1, "Bank A", time.Now()}},
		closeErr: closeErr,
	}
	store := &PostgresStore{db: &fakeDB{rows: rows}}

	_, err := store.History(context.Background(), "USD", 10)
	if !errors.Is(err, closeErr) {
		t.Fatalf("History() error = %v, want close error", err)
	}
	if !strings.Contains(err.Error(), "close rate history rows") {
		t.Fatalf("History() error = %q, want close context", err)
	}
	if !rows.closed {
		t.Fatal("rows were not closed")
	}
}

func TestPostgresStoreHistoryReportsRowsCloseErrorWithScanError(t *testing.T) {
	scanErr := errors.New("scan failed")
	closeErr := errors.New("close failed")
	rows := &fakeRows{
		values:   [][]any{{"USD", 91.2, 92.1, "Bank A", time.Now()}},
		scanErr:  scanErr,
		closeErr: closeErr,
	}
	store := &PostgresStore{db: &fakeDB{rows: rows}}

	_, err := store.History(context.Background(), "USD", 10)
	if !errors.Is(err, scanErr) {
		t.Fatalf("History() error = %v, want scan error", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("History() error = %v, want close error", err)
	}
	if !strings.Contains(err.Error(), "scan rate history row") {
		t.Fatalf("History() error = %q, want scan context", err)
	}
	if !strings.Contains(err.Error(), "close rate history rows") {
		t.Fatalf("History() error = %q, want close context", err)
	}
	if !rows.closed {
		t.Fatal("rows were not closed after scan error")
	}
}

func TestPostgresStoreHistoryReportsRowsCloseErrorWithRowsError(t *testing.T) {
	rowsErr := errors.New("rows failed")
	closeErr := errors.New("close failed")
	rows := &fakeRows{err: rowsErr, closeErr: closeErr}
	store := &PostgresStore{db: &fakeDB{rows: rows}}

	_, err := store.History(context.Background(), "USD", 10)
	if !errors.Is(err, rowsErr) {
		t.Fatalf("History() error = %v, want rows error", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("History() error = %v, want close error", err)
	}
	if !strings.Contains(err.Error(), "read rate history rows") {
		t.Fatalf("History() error = %q, want rows context", err)
	}
	if !strings.Contains(err.Error(), "close rate history rows") {
		t.Fatalf("History() error = %q, want close context", err)
	}
	if !rows.closed {
		t.Fatal("rows were not closed after rows error")
	}
}
