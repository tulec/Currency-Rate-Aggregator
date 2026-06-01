package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"testing"
)

const openTestDriverName = "postgres-open-test"

var openTestState = struct {
	sync.Mutex
	openNames  []string
	pingCount  int
	pingErr    error
	execs      []string
	execErr    error
	closeCount int
	closeErr   error
}{}

func init() {
	sql.Register(openTestDriverName, openTestDriver{})
}

func TestOpenPostgresStoreSkipsEmptyDatabaseURL(t *testing.T) {
	store, closeStore, err := openPostgresStore(context.Background(), openTestDriverName, "")
	if err != nil {
		t.Fatalf("openPostgresStore() error = %v", err)
	}
	if store != nil {
		t.Fatal("openPostgresStore() store is not nil, want nil")
	}
	if err := closeStore(); err != nil {
		t.Fatalf("closeStore() error = %v", err)
	}
}

func TestOpenPostgresStoreSkipsWhitespaceOnlyDatabaseURL(t *testing.T) {
	resetOpenTestState()

	store, closeStore, err := openPostgresStore(context.Background(), openTestDriverName, " \t ")
	if err != nil {
		t.Fatalf("openPostgresStore() error = %v", err)
	}
	if store != nil {
		t.Fatal("openPostgresStore() store is not nil, want nil")
	}
	if err := closeStore(); err != nil {
		t.Fatalf("closeStore() error = %v", err)
	}

	state := snapshotOpenTestState()
	if len(state.openNames) != 0 {
		t.Fatalf("opened data sources = %v, want none", state.openNames)
	}
	if state.pingCount != 0 {
		t.Fatalf("ping count = %d, want 0", state.pingCount)
	}
}

func TestOpenPostgresStoreReportsMissingDriver(t *testing.T) {
	store, closeStore, err := openPostgresStore(context.Background(), "missing-postgres-driver-test", "postgres://localhost/rates")
	if !errors.Is(err, ErrPostgresDriverUnavailable) {
		t.Fatalf("openPostgresStore() error = %v, want ErrPostgresDriverUnavailable", err)
	}
	if store != nil {
		t.Fatal("openPostgresStore() store is not nil after missing driver")
	}
	if err := closeStore(); err != nil {
		t.Fatalf("closeStore() error = %v", err)
	}
}

func TestOpenPostgresStoreMigratesAndReturnsCloseFunc(t *testing.T) {
	resetOpenTestState()

	dsn := "postgres://user:pass@localhost:5432/rates?sslmode=disable"
	store, closeStore, err := openPostgresStore(context.Background(), openTestDriverName, dsn)
	if err != nil {
		t.Fatalf("openPostgresStore() error = %v", err)
	}
	if store == nil {
		t.Fatal("openPostgresStore() store is nil")
	}

	state := snapshotOpenTestState()
	if len(state.openNames) != 1 || state.openNames[0] != dsn {
		t.Fatalf("opened data sources = %v, want [%s]", state.openNames, dsn)
	}
	if state.pingCount != 1 {
		t.Fatalf("ping count = %d, want 1", state.pingCount)
	}
	if len(state.execs) != 2 {
		t.Fatalf("migration statements = %d, want 2", len(state.execs))
	}
	if !strings.Contains(state.execs[0], "BIGSERIAL PRIMARY KEY") {
		t.Fatalf("first migration query = %q, want postgres id", state.execs[0])
	}

	if err := closeStore(); err != nil {
		t.Fatalf("closeStore() error = %v", err)
	}

	state = snapshotOpenTestState()
	if state.closeCount != 1 {
		t.Fatalf("closed connections = %d, want 1", state.closeCount)
	}
}

func TestOpenPostgresStoreClosesDatabaseOnPingError(t *testing.T) {
	pingErr := errors.New("database unavailable")
	resetOpenTestState()
	openTestState.Lock()
	openTestState.pingErr = pingErr
	openTestState.Unlock()

	store, _, err := openPostgresStore(context.Background(), openTestDriverName, "postgres://localhost/unavailable")
	if !errors.Is(err, pingErr) {
		t.Fatalf("openPostgresStore() error = %v, want ping error", err)
	}
	if store != nil {
		t.Fatal("openPostgresStore() store is not nil after ping error")
	}
	if !strings.Contains(err.Error(), "ping postgres") {
		t.Fatalf("openPostgresStore() error = %q, want ping context", err)
	}

	state := snapshotOpenTestState()
	if state.pingCount != 1 {
		t.Fatalf("ping count = %d, want 1", state.pingCount)
	}
	if len(state.execs) != 0 {
		t.Fatalf("migration statements = %d, want 0 after ping error", len(state.execs))
	}
	if state.closeCount != 1 {
		t.Fatalf("closed connections = %d, want 1", state.closeCount)
	}
}

func TestOpenPostgresStoreReportsCloseErrorAfterPingError(t *testing.T) {
	pingErr := errors.New("database unavailable")
	closeErr := errors.New("close failed")
	resetOpenTestState()
	openTestState.Lock()
	openTestState.pingErr = pingErr
	openTestState.closeErr = closeErr
	openTestState.Unlock()

	store, _, err := openPostgresStore(context.Background(), openTestDriverName, "postgres://localhost/unavailable")
	if !errors.Is(err, pingErr) {
		t.Fatalf("openPostgresStore() error = %v, want ping error", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("openPostgresStore() error = %v, want close error", err)
	}
	if store != nil {
		t.Fatal("openPostgresStore() store is not nil after ping error")
	}
	if !strings.Contains(err.Error(), "close postgres after setup failure") {
		t.Fatalf("openPostgresStore() error = %q, want close context", err)
	}
}

func TestOpenPostgresStoreClosesDatabaseOnMigrationError(t *testing.T) {
	migrateErr := errors.New("migration failed")
	resetOpenTestState()
	openTestState.Lock()
	openTestState.execErr = migrateErr
	openTestState.Unlock()

	store, _, err := openPostgresStore(context.Background(), openTestDriverName, "postgres://localhost/bad")
	if !errors.Is(err, migrateErr) {
		t.Fatalf("openPostgresStore() error = %v, want migration error", err)
	}
	if store != nil {
		t.Fatal("openPostgresStore() store is not nil after migration error")
	}

	state := snapshotOpenTestState()
	if state.closeCount != 1 {
		t.Fatalf("closed connections = %d, want 1", state.closeCount)
	}
}

func TestOpenPostgresStoreReportsCloseErrorAfterMigrationError(t *testing.T) {
	migrateErr := errors.New("migration failed")
	closeErr := errors.New("close failed")
	resetOpenTestState()
	openTestState.Lock()
	openTestState.execErr = migrateErr
	openTestState.closeErr = closeErr
	openTestState.Unlock()

	store, _, err := openPostgresStore(context.Background(), openTestDriverName, "postgres://localhost/bad")
	if !errors.Is(err, migrateErr) {
		t.Fatalf("openPostgresStore() error = %v, want migration error", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("openPostgresStore() error = %v, want close error", err)
	}
	if store != nil {
		t.Fatal("openPostgresStore() store is not nil after migration error")
	}
	if !strings.Contains(err.Error(), "close postgres after setup failure") {
		t.Fatalf("openPostgresStore() error = %q, want close context", err)
	}
}

func resetOpenTestState() {
	openTestState.Lock()
	defer openTestState.Unlock()

	openTestState.openNames = nil
	openTestState.pingCount = 0
	openTestState.pingErr = nil
	openTestState.execs = nil
	openTestState.execErr = nil
	openTestState.closeCount = 0
	openTestState.closeErr = nil
}

type openStateSnapshot struct {
	openNames  []string
	pingCount  int
	execs      []string
	closeCount int
}

func snapshotOpenTestState() openStateSnapshot {
	openTestState.Lock()
	defer openTestState.Unlock()

	return openStateSnapshot{
		openNames:  append([]string(nil), openTestState.openNames...),
		pingCount:  openTestState.pingCount,
		execs:      append([]string(nil), openTestState.execs...),
		closeCount: openTestState.closeCount,
	}
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

func (openTestConn) ExecContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	openTestState.Lock()
	defer openTestState.Unlock()

	openTestState.execs = append(openTestState.execs, query)
	if openTestState.execErr != nil {
		return nil, openTestState.execErr
	}
	return driver.RowsAffected(0), nil
}
