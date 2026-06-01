package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type fakeDB struct {
	execs    []fakeExec
	execErr  error
	tx       *fakeTx
	beginErr error
	query    fakeExec
	queryErr error
	rows     *fakeRows
}

func (d *fakeDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.execs = append(d.execs, fakeExec{query: query, args: args})
	if d.execErr != nil {
		return nil, d.execErr
	}
	return nil, nil
}

func (d *fakeDB) QueryContext(ctx context.Context, query string, args ...any) (rowScanner, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.query = fakeExec{query: query, args: args}
	if d.queryErr != nil {
		return nil, d.queryErr
	}
	if d.rows == nil {
		d.rows = &fakeRows{}
	}
	return d.rows, nil
}

func (d *fakeDB) BeginTx(ctx context.Context, _ *sql.TxOptions) (txRunner, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	if d.tx == nil {
		d.tx = &fakeTx{}
	}
	return d.tx, nil
}

type fakeTx struct {
	execs       []fakeExec
	execErr     error
	commitErr   error
	rollbackErr error
	committed   bool
	rolledBack  bool
}

func (tx *fakeTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tx.execs = append(tx.execs, fakeExec{query: query, args: args})
	if tx.execErr != nil {
		return nil, tx.execErr
	}
	return nil, nil
}

func (tx *fakeTx) Commit() error {
	if tx.commitErr != nil {
		return tx.commitErr
	}
	tx.committed = true
	return nil
}

func (tx *fakeTx) Rollback() error {
	tx.rolledBack = true
	return tx.rollbackErr
}

type fakeExec struct {
	query string
	args  []any
}

type fakeRows struct {
	values   [][]any
	index    int
	closed   bool
	scanErr  error
	err      error
	closeErr error
}

func (r *fakeRows) Next() bool {
	if r.index >= len(r.values) {
		return false
	}
	r.index++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	row := r.values[r.index-1]
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			*target = row[i].(string)
		case *float64:
			*target = row[i].(float64)
		case *time.Time:
			*target = row[i].(time.Time)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}

func (r *fakeRows) Err() error {
	return r.err
}

func (r *fakeRows) Close() error {
	r.closed = true
	return r.closeErr
}
