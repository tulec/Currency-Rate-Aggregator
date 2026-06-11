package storage

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"currency-rate-aggregator/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestPostgresStoreMigrateRunsGooseMigrator(t *testing.T) {
	migrator := &fakeSchemaMigrator{}
	store := &PostgresStore{db: &fakeDB{}, migrations: migrator}

	if err := store.Migrate(context.Background()); err != nil {
		require.FailNowf(t, "test failed", "Migrate() error = %v", err)
	}
	require.EqualValuesf(t, 1, migrator.calls,
		"migration runs = %d, want 1", migrator.calls)

}

func TestPostgresStoreMigrateWrapsGooseError(t *testing.T) {
	migrateErr := errors.New("migration failed")
	store := &PostgresStore{
		db:         &fakeDB{},
		migrations: &fakeSchemaMigrator{err: migrateErr},
	}

	err := store.Migrate(context.Background())
	require.ErrorIsf(t, err, migrateErr,
		"Migrate() error = %v, want migration error", err)
	require.Containsf(t, err.Error(), "apply postgres migrations with goose",
		"Migrate() error = %q, want goose context", err)

}

func TestGooseProviderLoadsEmbeddedPostgresMigrations(t *testing.T) {
	resetOpenTestState()
	defer resetOpenTestState()

	db, err := sql.Open(openTestDriverName, "embedded-migrations")
	require.NoErrorf(t, err,
		"sql.Open() error = %v", err)

	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {

		}
	}(db)

	provider, err := newGooseProvider(db)
	require.NoErrorf(t, err,
		"newGooseProvider() error = %v", err)

	sources := provider.ListSources()
	require.Lenf(t, sources, 2,
		"goose migration sources = %d, want 2", len(sources))
	require.Falsef(t, sources[0].Version != 1 || sources[1].Version != 2,
		"goose migration versions = [%d %d], want [1 2]", sources[0].Version, sources[1].Version)

	assertGooseMigrationFile(t, "migrations/001_create_rates_table.sql", "BIGSERIAL PRIMARY KEY", "DROP TABLE IF EXISTS rates")
	assertGooseMigrationFile(t, "migrations/002_create_rates_index.sql", "CREATE INDEX IF NOT EXISTS idx_rates_currency_fetched_at", "DROP INDEX IF EXISTS idx_rates_currency_fetched_at")
}

func assertGooseMigrationFile(t *testing.T, name string, upSQL string, downSQL string) {
	t.Helper()

	content, err := fs.ReadFile(postgresMigrations, name)
	require.NoErrorf(t, err,
		"read migration %s: %v", name, err)

	text := string(content)
	for _, expected := range []string{"-- +goose Up", "-- +goose Down", upSQL, downSQL} {
		require.Containsf(t, text, expected,
			"migration %s does not contain %q", name, expected)

	}
}

func TestPostgresStoreReportsUnconfiguredStore(t *testing.T) {
	var nilStore *PostgresStore
	if err := nilStore.Migrate(context.Background()); !errors.Is(err, ErrStoreNotConfigured) {
		require.FailNowf(t, "test failed", "nil Migrate() error = %v, want ErrStoreNotConfigured", err)
	}

	store := NewPostgresStore(nil)
	if err := store.Migrate(context.Background()); !errors.Is(err, ErrStoreNotConfigured) {
		require.FailNowf(t, "test failed", "Migrate() error = %v, want ErrStoreNotConfigured", err)
	}
	if err := store.SaveRates(context.Background(), []domain.CurrencyRate{{Currency: "USD", Buy: 91.2, Sell: 92.1, Bank: "Bank A", FetchedAt: time.Now()}}); !errors.Is(err, ErrStoreNotConfigured) {
		require.FailNowf(t, "test failed", "SaveRates() error = %v, want ErrStoreNotConfigured", err)
	}
	if err := store.SaveRates(context.Background(), nil); !errors.Is(err, ErrStoreNotConfigured) {
		require.FailNowf(t, "test failed", "SaveRates() with empty rates error = %v, want ErrStoreNotConfigured", err)
	}
	if _, err := store.History(context.Background(), "USD", 10); !errors.Is(err, ErrStoreNotConfigured) {
		require.FailNowf(t, "test failed", "History() error = %v, want ErrStoreNotConfigured", err)
	}
	if _, err := store.HistoryByDate(context.Background(), "USD", time.Now(), time.Now().Add(time.Hour), 10); !errors.Is(err, ErrStoreNotConfigured) {
		require.FailNowf(t, "test failed", "HistoryByDate() error = %v, want ErrStoreNotConfigured", err)
	}
}

type fakeSchemaMigrator struct {
	calls int
	err   error
}

func (m *fakeSchemaMigrator) Up(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.calls++
	return m.err
}

func TestPostgresStoreSaveRatesInsertsRatesTransactionally(t *testing.T) {
	tx := &fakeTx{}
	db := &fakeDB{tx: tx}
	store := &PostgresStore{db: db}
	fetchedAt := time.Date(2026, 5, 18, 10, 0, 0, 123, time.FixedZone("MSK", 3*60*60))

	err := store.SaveRates(context.Background(), []domain.CurrencyRate{
		{Currency: " usd ", Buy: 91.2, Sell: 92.1, Bank: "Bank A", FetchedAt: fetchedAt},
	})
	require.NoErrorf(t, err,
		"SaveRates() error = %v", err)
	require.True(t, tx.committed,
		"transaction was not committed")
	require.Lenf(t, tx.execs, 1,
		"insert statements = %d, want 1", len(tx.execs))
	require.Containsf(t, tx.execs[0].query, "VALUES ($1, $2, $3, $4, $5)",
		"insert query = %q, want postgres placeholders", tx.execs[0].query)
	require.EqualValuesf(t, "USD", tx.execs[0].args[0],
		"stored currency = %v, want USD", tx.execs[0].args[0])
	require.EqualValuesf(t, fetchedAt.UTC(), tx.execs[0].args[4],
		"stored fetched_at = %v, want UTC time", tx.execs[0].args[4])

}

func TestPostgresStoreSaveRatesRollsBackOnInsertError(t *testing.T) {
	insertErr := errors.New("insert failed")
	tx := &fakeTx{execErr: insertErr}
	db := &fakeDB{tx: tx}
	store := &PostgresStore{db: db}

	err := store.SaveRates(context.Background(), []domain.CurrencyRate{
		{Currency: "USD", Buy: 91.2, Sell: 92.1, Bank: "Bank A", FetchedAt: time.Now()},
	})
	require.ErrorIsf(t, err, insertErr,
		"SaveRates() error = %v, want insert error", err)
	require.True(t, tx.rolledBack,
		"transaction was not rolled back")
	require.False(t, tx.committed,
		"transaction was committed after insert error")

}

func TestPostgresStoreSaveRatesReportsRollbackError(t *testing.T) {
	insertErr := errors.New("insert failed")
	rollbackErr := errors.New("rollback failed")
	tx := &fakeTx{execErr: insertErr, rollbackErr: rollbackErr}
	store := &PostgresStore{db: &fakeDB{tx: tx}}

	err := store.SaveRates(context.Background(), []domain.CurrencyRate{
		{Currency: "USD", Buy: 91.2, Sell: 92.1, Bank: "Bank A", FetchedAt: time.Now()},
	})
	require.ErrorIsf(t, err, insertErr,
		"SaveRates() error = %v, want insert error", err)
	require.ErrorIsf(t, err, rollbackErr,
		"SaveRates() error = %v, want rollback error", err)
	require.Containsf(t, err.Error(), "rollback save rates transaction",
		"SaveRates() error = %q, want rollback context", err)
	require.True(t, tx.rolledBack,
		"transaction was not rolled back")
	require.False(t, tx.committed,
		"transaction was committed after insert error")

}

func TestPostgresStoreSaveRatesReportsBeginError(t *testing.T) {
	beginErr := errors.New("begin failed")
	store := &PostgresStore{db: &fakeDB{beginErr: beginErr}}

	err := store.SaveRates(context.Background(), []domain.CurrencyRate{
		{Currency: "USD", Buy: 91.2, Sell: 92.1, Bank: "Bank A", FetchedAt: time.Now()},
	})
	require.ErrorIsf(t, err, beginErr,
		"SaveRates() error = %v, want begin error", err)
	require.Containsf(t, err.Error(), "begin save rates transaction",
		"SaveRates() error = %q, want begin context", err)

}

func TestPostgresStoreSaveRatesReportsCommitError(t *testing.T) {
	commitErr := errors.New("commit failed")
	tx := &fakeTx{commitErr: commitErr}
	store := &PostgresStore{db: &fakeDB{tx: tx}}

	err := store.SaveRates(context.Background(), []domain.CurrencyRate{
		{Currency: "USD", Buy: 91.2, Sell: 92.1, Bank: "Bank A", FetchedAt: time.Now()},
	})
	require.ErrorIsf(t, err, commitErr,
		"SaveRates() error = %v, want commit error", err)
	require.Containsf(t, err.Error(), "commit save rates transaction",
		"SaveRates() error = %q, want commit context", err)
	require.True(t, tx.rolledBack,
		"transaction was not rolled back after commit error")

}

func TestPostgresStoreSaveRatesReportsInvalidCurrencyContext(t *testing.T) {
	tx := &fakeTx{}
	store := &PostgresStore{db: &fakeDB{tx: tx}}

	err := store.SaveRates(context.Background(), []domain.CurrencyRate{
		{Currency: "USDT", Buy: 91.2, Sell: 92.1, Bank: "Bank A", FetchedAt: time.Now()},
	})
	require.ErrorIsf(t, err, domain.ErrInvalidCurrencyCode,
		"SaveRates() error = %v, want ErrInvalidCurrencyCode", err)
	require.Containsf(t, err.Error(), `normalize rate currency "USDT" from Bank A`,
		"SaveRates() error = %q, want currency and bank context", err)
	require.True(t, tx.rolledBack,
		"transaction was not rolled back")
	require.False(t, tx.committed,
		"transaction was committed after invalid currency")
	require.Lenf(t, tx.execs, 0,
		"insert statements = %d, want 0", len(tx.execs))

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
	require.NoErrorf(t, err,
		"History() error = %v", err)
	require.Lenf(t, rates, 1,
		"rates = %d, want 1", len(rates))
	require.EqualValuesf(t, fetchedAt.UTC(), rates[0].FetchedAt,
		"FetchedAt = %v, want %v", rates[0].FetchedAt, fetchedAt.UTC())
	require.EqualValuesf(t, "USD", db.query.args[0],
		"query currency = %v, want USD", db.query.args[0])
	require.EqualValuesf(t, MaxHistoryLimit, db.query.args[1],
		"query limit = %v, want %d", db.query.args[1], MaxHistoryLimit)
	require.Containsf(t, db.query.query, "WHERE currency = $1",
		"history query = %q, want postgres placeholders", db.query.query)
	require.True(t, rows.closed,
		"rows were not closed")

}

func TestPostgresStoreHistoryReturnsEmptySliceWhenNoRows(t *testing.T) {
	store := &PostgresStore{db: &fakeDB{}}

	rates, err := store.History(context.Background(), "USD", 10)
	require.NoErrorf(t, err,
		"History() error = %v", err)
	require.NotNil(t, rates,
		"History() rates is nil, want empty slice")
	require.Lenf(t, rates, 0,
		"rates = %d, want 0", len(rates))

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
	require.NoErrorf(t, err,
		"HistoryByDate() error = %v", err)
	require.Lenf(t, rates, 1,
		"rates = %d, want 1", len(rates))
	require.EqualValuesf(t, fetchedAt.UTC(), rates[0].FetchedAt,
		"FetchedAt = %v, want %v", rates[0].FetchedAt, fetchedAt.UTC())
	require.EqualValuesf(t, "USD", db.query.args[0],
		"query currency = %v, want USD", db.query.args[0])
	require.EqualValuesf(t, from.UTC(), db.query.args[1],
		"query from = %v, want %v", db.query.args[1], from.UTC())
	require.EqualValuesf(t, to.UTC(), db.query.args[2],
		"query to = %v, want %v", db.query.args[2], to.UTC())
	require.EqualValuesf(t, MaxHistoryLimit, db.query.args[3],
		"query limit = %v, want %d", db.query.args[3], MaxHistoryLimit)
	require.Falsef(t, !strings.Contains(db.query.query, "fetched_at >= $2") || !strings.Contains(db.query.query, "fetched_at < $3"),
		"history by date query = %q, want date range filter", db.query.query)
	require.True(t, rows.closed,
		"rows were not closed")

}

func TestPostgresStoreHistoryByDateRejectsInvalidCurrency(t *testing.T) {
	store := &PostgresStore{db: &fakeDB{}}

	_, err := store.HistoryByDate(context.Background(), "USDT", time.Now(), time.Now().Add(time.Hour), 10)
	require.ErrorIsf(t, err, domain.ErrInvalidCurrencyCode,
		"HistoryByDate() error = %v, want ErrInvalidCurrencyCode", err)
	require.Containsf(t, err.Error(), `normalize history currency "USDT"`,
		"HistoryByDate() error = %q, want currency context", err)

}

func TestPostgresStoreHistoryRejectsInvalidCurrency(t *testing.T) {
	store := &PostgresStore{db: &fakeDB{}}

	_, err := store.History(context.Background(), "USDT", 10)
	require.ErrorIsf(t, err, domain.ErrInvalidCurrencyCode,
		"History() error = %v, want ErrInvalidCurrencyCode", err)
	require.Containsf(t, err.Error(), `normalize history currency "USDT"`,
		"History() error = %q, want currency context", err)

}

func TestPostgresStoreHistoryReportsQueryError(t *testing.T) {
	queryErr := errors.New("query failed")
	store := &PostgresStore{db: &fakeDB{queryErr: queryErr}}

	_, err := store.History(context.Background(), "USD", 10)
	require.ErrorIsf(t, err, queryErr,
		"History() error = %v, want query error", err)
	require.Containsf(t, err.Error(), "query rate history",
		"History() error = %q, want query context", err)

}

func TestPostgresStoreHistoryReportsScanError(t *testing.T) {
	scanErr := errors.New("scan failed")
	rows := &fakeRows{
		values:  [][]any{{"USD", 91.2, 92.1, "Bank A", time.Now()}},
		scanErr: scanErr,
	}
	store := &PostgresStore{db: &fakeDB{rows: rows}}

	_, err := store.History(context.Background(), "USD", 10)
	require.ErrorIsf(t, err, scanErr,
		"History() error = %v, want scan error", err)
	require.Containsf(t, err.Error(), "scan rate history row",
		"History() error = %q, want scan context", err)
	require.True(t, rows.closed,
		"rows were not closed after scan error")

}

func TestPostgresStoreHistoryReportsRowsError(t *testing.T) {
	rowsErr := errors.New("rows failed")
	rows := &fakeRows{err: rowsErr}
	store := &PostgresStore{db: &fakeDB{rows: rows}}

	_, err := store.History(context.Background(), "USD", 10)
	require.ErrorIsf(t, err, rowsErr,
		"History() error = %v, want rows error", err)
	require.Containsf(t, err.Error(), "read rate history rows",
		"History() error = %q, want rows context", err)
	require.True(t, rows.closed,
		"rows were not closed after rows error")

}

func TestPostgresStoreHistoryReportsRowsCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	rows := &fakeRows{
		values:   [][]any{{"USD", 91.2, 92.1, "Bank A", time.Now()}},
		closeErr: closeErr,
	}
	store := &PostgresStore{db: &fakeDB{rows: rows}}

	_, err := store.History(context.Background(), "USD", 10)
	require.ErrorIsf(t, err, closeErr,
		"History() error = %v, want close error", err)
	require.Containsf(t, err.Error(), "close rate history rows",
		"History() error = %q, want close context", err)
	require.True(t, rows.closed,
		"rows were not closed")

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
	require.ErrorIsf(t, err, scanErr,
		"History() error = %v, want scan error", err)
	require.ErrorIsf(t, err, closeErr,
		"History() error = %v, want close error", err)
	require.Containsf(t, err.Error(), "scan rate history row",
		"History() error = %q, want scan context", err)
	require.Containsf(t, err.Error(), "close rate history rows",
		"History() error = %q, want close context", err)
	require.True(t, rows.closed,
		"rows were not closed after scan error")

}

func TestPostgresStoreHistoryReportsRowsCloseErrorWithRowsError(t *testing.T) {
	rowsErr := errors.New("rows failed")
	closeErr := errors.New("close failed")
	rows := &fakeRows{err: rowsErr, closeErr: closeErr}
	store := &PostgresStore{db: &fakeDB{rows: rows}}

	_, err := store.History(context.Background(), "USD", 10)
	require.ErrorIsf(t, err, rowsErr,
		"History() error = %v, want rows error", err)
	require.ErrorIsf(t, err, closeErr,
		"History() error = %v, want close error", err)
	require.Containsf(t, err.Error(), "read rate history rows",
		"History() error = %q, want rows context", err)
	require.Containsf(t, err.Error(), "close rate history rows",
		"History() error = %q, want close context", err)
	require.True(t, rows.closed,
		"rows were not closed after rows error")

}
