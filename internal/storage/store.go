package storage

import (
	"context"
	"database/sql"
	"errors"
)

const (
	DefaultHistoryLimit = 50
	MaxHistoryLimit     = 500
)

var ErrStoreNotConfigured = errors.New("rate store is not configured")

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return DefaultHistoryLimit
	}
	if limit > MaxHistoryLimit {
		return MaxHistoryLimit
	}
	return limit
}

type dbRunner interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (rowScanner, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (txRunner, error)
}

type txRunner interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	Commit() error
	Rollback() error
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

type sqlDB struct {
	db *sql.DB
}

func (d sqlDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if d.db == nil {
		return nil, ErrStoreNotConfigured
	}
	return d.db.ExecContext(ctx, query, args...)
}

func (d sqlDB) QueryContext(ctx context.Context, query string, args ...any) (rowScanner, error) {
	if d.db == nil {
		return nil, ErrStoreNotConfigured
	}
	return d.db.QueryContext(ctx, query, args...)
}

func (d sqlDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (txRunner, error) {
	if d.db == nil {
		return nil, ErrStoreNotConfigured
	}
	tx, err := d.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return tx, nil
}
