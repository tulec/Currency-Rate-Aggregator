package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "github.com/lib/pq"
)

const postgresDriverName = "postgres"

var ErrPostgresDriverUnavailable = errors.New("postgres driver is not registered")

func OpenPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, func() error, error) {
	return openPostgresStore(ctx, postgresDriverName, databaseURL)
}

func openPostgresStore(ctx context.Context, driverName, databaseURL string) (*PostgresStore, func() error, error) {
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return nil, func() error { return nil }, nil
	}

	db, err := sql.Open(driverName, databaseURL)
	if err != nil {
		if strings.Contains(err.Error(), "unknown driver") {
			return nil, func() error { return nil }, fmt.Errorf("%w: %v", ErrPostgresDriverUnavailable, err)
		}
		return nil, func() error { return nil }, err
	}

	if err := db.PingContext(ctx); err != nil {
		return nil, func() error { return nil }, closeDBAfterOpenError(db, fmt.Errorf("ping postgres: %w", err))
	}

	store := NewPostgresStore(db)
	if err := store.Migrate(ctx); err != nil {
		return nil, func() error { return nil }, closeDBAfterOpenError(db, err)
	}

	return store, db.Close, nil
}

func closeDBAfterOpenError(db *sql.DB, err error) error {
	if closeErr := db.Close(); closeErr != nil {
		return errors.Join(err, fmt.Errorf("close postgres after setup failure: %w", closeErr))
	}
	return err
}
