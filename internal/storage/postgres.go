package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"currency-rate-aggregator/internal/domain"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var postgresMigrations embed.FS

const postgresMigrationsDir = "migrations"

// noinspection SqlResolve
const postgresInsertRateSQL = `
INSERT INTO rates (currency, buy, sell, bank, fetched_at)
VALUES ($1, $2, $3, $4, $5)`

// noinspection SqlResolve
const postgresHistorySQL = `
SELECT currency, buy, sell, bank, fetched_at
FROM rates
WHERE currency = $1
ORDER BY fetched_at DESC, id DESC
LIMIT $2`

// noinspection SqlResolve
const postgresHistoryByDateSQL = `
SELECT currency, buy, sell, bank, fetched_at
FROM rates
WHERE currency = $1
  AND fetched_at >= $2
  AND fetched_at < $3
ORDER BY fetched_at DESC, id DESC
LIMIT $4`

type PostgresStore struct {
	db         dbRunner
	migrations schemaMigrator
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	if db == nil {
		return &PostgresStore{}
	}
	return &PostgresStore{
		db:         sqlDB{db: db},
		migrations: gooseSchemaMigrator{db: db},
	}
}

func (s *PostgresStore) Migrate(ctx context.Context) error {
	if s == nil || s.db == nil || s.migrations == nil {
		return ErrStoreNotConfigured
	}

	if err := s.migrations.Up(ctx); err != nil {
		return fmt.Errorf("apply postgres migrations with goose: %w", err)
	}

	return nil
}

type schemaMigrator interface {
	Up(ctx context.Context) error
}

type gooseSchemaMigrator struct {
	db *sql.DB
}

func (m gooseSchemaMigrator) Up(ctx context.Context) error {
	provider, err := newGooseProvider(m.db)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

func newGooseProvider(db *sql.DB) (*goose.Provider, error) {
	migrations, err := fs.Sub(postgresMigrations, postgresMigrationsDir)
	if err != nil {
		return nil, fmt.Errorf("open embedded postgres migrations: %w", err)
	}

	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrations,
		goose.WithLogger(goose.NopLogger()),
	)
	if err != nil {
		return nil, fmt.Errorf("create postgres goose provider: %w", err)
	}
	return provider, nil
}

func (s *PostgresStore) SaveRates(ctx context.Context, rates []domain.CurrencyRate) (err error) {
	if s == nil || s.db == nil {
		return ErrStoreNotConfigured
	}
	if len(rates) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin save rates transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				rollbackErr = fmt.Errorf("rollback save rates transaction: %w", rollbackErr)
				if err != nil {
					err = errors.Join(err, rollbackErr)
					return
				}
				err = rollbackErr
			}
		}
	}()

	for _, rate := range rates {
		currency, err := domain.NormalizeCurrency(rate.Currency.String())
		if err != nil {
			return fmt.Errorf("normalize rate currency %q from %s: %w", rate.Currency, rate.Bank, err)
		}

		if _, err := tx.ExecContext(
			ctx,
			postgresInsertRateSQL,
			currency,
			rate.Buy,
			rate.Sell,
			rate.Bank,
			rate.FetchedAt.UTC(),
		); err != nil {
			return fmt.Errorf("insert rate for %s from %s: %w", currency, rate.Bank, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit save rates transaction: %w", err)
	}
	committed = true
	return nil
}

func (s *PostgresStore) History(ctx context.Context, currency string, limit int) (rates []domain.CurrencyRate, err error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreNotConfigured
	}

	normalized, err := domain.NormalizeCurrency(currency)
	if err != nil {
		return nil, fmt.Errorf("normalize history currency %q: %w", currency, err)
	}

	rows, err := s.db.QueryContext(ctx, postgresHistorySQL, normalized, normalizeLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("query rate history: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			closeErr = fmt.Errorf("close rate history rows: %w", closeErr)
			if err != nil {
				err = errors.Join(err, closeErr)
				return
			}
			err = closeErr
		}
	}()

	rates = make([]domain.CurrencyRate, 0)
	for rows.Next() {
		var rate domain.CurrencyRate
		var storedCurrency string
		var fetchedAt time.Time

		if err := rows.Scan(&storedCurrency, &rate.Buy, &rate.Sell, &rate.Bank, &fetchedAt); err != nil {
			return nil, fmt.Errorf("scan rate history row: %w", err)
		}

		currency, err := domain.NormalizeCurrency(storedCurrency)
		if err != nil {
			return nil, fmt.Errorf("normalize stored history currency %q from %s: %w", storedCurrency, rate.Bank, err)
		}
		rate.Currency = domain.CurrencyCode(currency)
		rate.FetchedAt = fetchedAt.UTC()
		rates = append(rates, rate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read rate history rows: %w", err)
	}

	return rates, nil
}

func (s *PostgresStore) HistoryByDate(ctx context.Context, currency string, from, to time.Time, limit int) (rates []domain.CurrencyRate, err error) {
	if s == nil || s.db == nil {
		return nil, ErrStoreNotConfigured
	}

	normalized, err := domain.NormalizeCurrency(currency)
	if err != nil {
		return nil, fmt.Errorf("normalize history currency %q: %w", currency, err)
	}

	rows, err := s.db.QueryContext(ctx, postgresHistoryByDateSQL, normalized, from.UTC(), to.UTC(), normalizeLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("query rate history by date: %w", err)
	}
	return scanRateHistoryRows(rows, "rate history by date")
}

func scanRateHistoryRows(rows rowScanner, label string) (rates []domain.CurrencyRate, err error) {
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			closeErr = fmt.Errorf("close %s rows: %w", label, closeErr)
			if err != nil {
				err = errors.Join(err, closeErr)
				return
			}
			err = closeErr
		}
	}()

	rates = make([]domain.CurrencyRate, 0)
	for rows.Next() {
		var rate domain.CurrencyRate
		var storedCurrency string
		var fetchedAt time.Time

		if err := rows.Scan(&storedCurrency, &rate.Buy, &rate.Sell, &rate.Bank, &fetchedAt); err != nil {
			return nil, fmt.Errorf("scan %s row: %w", label, err)
		}

		currency, err := domain.NormalizeCurrency(storedCurrency)
		if err != nil {
			return nil, fmt.Errorf("normalize stored history currency %q from %s: %w", storedCurrency, rate.Bank, err)
		}
		rate.Currency = domain.CurrencyCode(currency)
		rate.FetchedAt = fetchedAt.UTC()
		rates = append(rates, rate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read %s rows: %w", label, err)
	}

	return rates, nil
}
