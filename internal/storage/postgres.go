package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"currency-rate-aggregator/internal/domain"
)

//go:embed migrations/*.sql
var postgresMigrations embed.FS

var postgresMigrationFiles = []string{
	"migrations/001_create_rates_table.sql",
	"migrations/002_create_rates_index.sql",
}

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

type PostgresStore struct {
	db dbRunner
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	if db == nil {
		return &PostgresStore{}
	}
	return &PostgresStore{db: sqlDB{db: db}}
}

func (s *PostgresStore) Migrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return ErrStoreNotConfigured
	}

	for _, file := range postgresMigrationFiles {
		query, err := postgresMigrations.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read postgres migration %s: %w", file, err)
		}
		if _, err := s.db.ExecContext(ctx, string(query)); err != nil {
			return fmt.Errorf("apply postgres migration %s: %w", file, err)
		}
	}

	return nil
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
		currency, err := domain.NormalizeCurrency(rate.Currency)
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
		var fetchedAt time.Time

		if err := rows.Scan(&rate.Currency, &rate.Buy, &rate.Sell, &rate.Bank, &fetchedAt); err != nil {
			return nil, fmt.Errorf("scan rate history row: %w", err)
		}

		currency, err := domain.NormalizeCurrency(rate.Currency)
		if err != nil {
			return nil, fmt.Errorf("normalize stored history currency %q from %s: %w", rate.Currency, rate.Bank, err)
		}
		rate.Currency = currency
		rate.FetchedAt = fetchedAt.UTC()
		rates = append(rates, rate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read rate history rows: %w", err)
	}

	return rates, nil
}
