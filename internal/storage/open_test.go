package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
)

const openTestDriverName = "postgres-open-test"

var openTestState = struct {
	sync.Mutex
	openNames    []string
	pingCount    int
	pingErr      error
	migrateCount int
	migrateErr   error
	closeCount   int
	closeErr     error
}{}

func init() {
	sql.Register(openTestDriverName, openTestDriver{})
}

func TestOpenPostgresStoreSkipsEmptyDatabaseURL(t *testing.T) {
	store, closeStore, err := openTestPostgresStore(context.Background(), "")
	require.NoErrorf(t, err,
		"openPostgresStore() error = %v", err)
	require.Nil(t, store,
		"openPostgresStore() store is not nil, want nil")

	if err := closeStore(); err != nil {
		require.FailNowf(t, "test failed", "closeStore() error = %v", err)
	}
}

func TestOpenPostgresStoreSkipsWhitespaceOnlyDatabaseURL(t *testing.T) {
	resetOpenTestState()

	store, closeStore, err := openTestPostgresStore(context.Background(), " \t ")
	require.NoErrorf(t, err,
		"openPostgresStore() error = %v", err)
	require.Nil(t, store,
		"openPostgresStore() store is not nil, want nil")

	if err := closeStore(); err != nil {
		require.FailNowf(t, "test failed", "closeStore() error = %v", err)
	}

	state := snapshotOpenTestState()
	require.Lenf(t, state.openNames, 0,
		"opened data sources = %v, want none", state.openNames)
	require.EqualValuesf(t, 0, state.pingCount,
		"ping count = %d, want 0", state.pingCount)

}

func TestOpenPostgresStoreReportsMissingDriver(t *testing.T) {
	store, closeStore, err := openPostgresStore(context.Background(), "missing-postgres-driver-test", "postgres://localhost/rates")
	require.ErrorIsf(t, err, ErrPostgresDriverUnavailable,
		"openPostgresStore() error = %v, want ErrPostgresDriverUnavailable", err)
	require.Nil(t, store,
		"openPostgresStore() store is not nil after missing driver")

	if err := closeStore(); err != nil {
		require.FailNowf(t, "test failed", "closeStore() error = %v", err)
	}
}

func TestOpenPostgresStoreMigratesAndReturnsCloseFunc(t *testing.T) {
	resetOpenTestState()

	dsn := "postgres://user:pass@localhost:5432/rates?sslmode=disable"
	store, closeStore, err := openTestPostgresStore(context.Background(), dsn)
	require.NoErrorf(t, err,
		"openPostgresStore() error = %v", err)
	require.NotNil(t, store,
		"openPostgresStore() store is nil")

	state := snapshotOpenTestState()
	require.Falsef(t, len(state.openNames) != 1 || state.openNames[0] != dsn,
		"opened data sources = %v, want [%s]", state.openNames, dsn)
	require.EqualValuesf(t, 1, state.pingCount,
		"ping count = %d, want 1", state.pingCount)
	require.EqualValuesf(t, 1, state.migrateCount,
		"migration runs = %d, want 1", state.migrateCount)

	if err := closeStore(); err != nil {
		require.FailNowf(t, "test failed", "closeStore() error = %v", err)
	}

	state = snapshotOpenTestState()
	require.EqualValuesf(t, 1, state.closeCount,
		"closed connections = %d, want 1", state.closeCount)

}

func TestOpenPostgresStoreClosesDatabaseOnPingError(t *testing.T) {
	pingErr := errors.New("database unavailable")
	resetOpenTestState()
	openTestState.Lock()
	openTestState.pingErr = pingErr
	openTestState.Unlock()

	store, _, err := openTestPostgresStore(context.Background(), "postgres://localhost/unavailable")
	require.ErrorIsf(t, err, pingErr,
		"openPostgresStore() error = %v, want ping error", err)
	require.Nil(t, store,
		"openPostgresStore() store is not nil after ping error")
	require.Containsf(t, err.Error(), "ping postgres",
		"openPostgresStore() error = %q, want ping context", err)

	state := snapshotOpenTestState()
	require.EqualValuesf(t, 1, state.pingCount,
		"ping count = %d, want 1", state.pingCount)
	require.EqualValuesf(t, 0, state.migrateCount,
		"migration runs = %d, want 0 after ping error", state.migrateCount)
	require.EqualValuesf(t, 1, state.closeCount,
		"closed connections = %d, want 1", state.closeCount)

}

func TestOpenPostgresStoreReportsCloseErrorAfterPingError(t *testing.T) {
	pingErr := errors.New("database unavailable")
	closeErr := errors.New("close failed")
	resetOpenTestState()
	openTestState.Lock()
	openTestState.pingErr = pingErr
	openTestState.closeErr = closeErr
	openTestState.Unlock()

	store, _, err := openTestPostgresStore(context.Background(), "postgres://localhost/unavailable")
	require.ErrorIsf(t, err, pingErr,
		"openPostgresStore() error = %v, want ping error", err)
	require.ErrorIsf(t, err, closeErr,
		"openPostgresStore() error = %v, want close error", err)
	require.Nil(t, store,
		"openPostgresStore() store is not nil after ping error")
	require.Containsf(t, err.Error(), "close postgres after setup failure",
		"openPostgresStore() error = %q, want close context", err)

}

func TestOpenPostgresStoreClosesDatabaseOnMigrationError(t *testing.T) {
	migrateErr := errors.New("migration failed")
	resetOpenTestState()
	openTestState.Lock()
	openTestState.migrateErr = migrateErr
	openTestState.Unlock()

	store, _, err := openTestPostgresStore(context.Background(), "postgres://localhost/bad")
	require.ErrorIsf(t, err, migrateErr,
		"openPostgresStore() error = %v, want migration error", err)
	require.Nil(t, store,
		"openPostgresStore() store is not nil after migration error")

	state := snapshotOpenTestState()
	require.EqualValuesf(t, 1, state.closeCount,
		"closed connections = %d, want 1", state.closeCount)

}

func TestOpenPostgresStoreReportsCloseErrorAfterMigrationError(t *testing.T) {
	migrateErr := errors.New("migration failed")
	closeErr := errors.New("close failed")
	resetOpenTestState()
	openTestState.Lock()
	openTestState.migrateErr = migrateErr
	openTestState.closeErr = closeErr
	openTestState.Unlock()

	store, _, err := openTestPostgresStore(context.Background(), "postgres://localhost/bad")
	require.ErrorIsf(t, err, migrateErr,
		"openPostgresStore() error = %v, want migration error", err)
	require.ErrorIsf(t, err, closeErr,
		"openPostgresStore() error = %v, want close error", err)
	require.Nil(t, store,
		"openPostgresStore() store is not nil after migration error")
	require.Containsf(t, err.Error(), "close postgres after setup failure",
		"openPostgresStore() error = %q, want close context", err)

}

func resetOpenTestState() {
	openTestState.Lock()
	defer openTestState.Unlock()

	openTestState.openNames = nil
	openTestState.pingCount = 0
	openTestState.pingErr = nil
	openTestState.migrateCount = 0
	openTestState.migrateErr = nil
	openTestState.closeCount = 0
	openTestState.closeErr = nil
}

type openStateSnapshot struct {
	openNames    []string
	pingCount    int
	migrateCount int
	closeCount   int
}

func snapshotOpenTestState() openStateSnapshot {
	openTestState.Lock()
	defer openTestState.Unlock()

	return openStateSnapshot{
		openNames:    append([]string(nil), openTestState.openNames...),
		pingCount:    openTestState.pingCount,
		migrateCount: openTestState.migrateCount,
		closeCount:   openTestState.closeCount,
	}
}

func openTestPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, func() error, error) {
	return openPostgresStoreWithFactory(ctx, openTestDriverName, databaseURL, func(db *sql.DB) *PostgresStore {
		store := NewPostgresStore(db)
		store.migrations = openTestMigrator{}
		return store
	})
}

type openTestMigrator struct{}

func (openTestMigrator) Up(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	openTestState.Lock()
	defer openTestState.Unlock()

	openTestState.migrateCount++
	return openTestState.migrateErr
}

type openTestDriver struct{}

func (openTestDriver) Open(name string) (driver.Conn, error) {
	openTestState.Lock()
	openTestState.openNames = append(openTestState.openNames, name)
	openTestState.Unlock()

	return openTestConn{}, nil
}

type openTestConn struct{}

func (openTestConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not supported by open test driver")
}

func (openTestConn) Close() error {
	openTestState.Lock()
	defer openTestState.Unlock()

	openTestState.closeCount++
	return openTestState.closeErr
}

func (openTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not supported by open test driver")
}

func (openTestConn) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	openTestState.Lock()
	defer openTestState.Unlock()

	openTestState.pingCount++
	return openTestState.pingErr
}

func (openTestConn) ExecContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return driver.RowsAffected(0), nil
}
