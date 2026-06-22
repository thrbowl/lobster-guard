package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5/stdlib"
)

const postgresCompatDriverName = "lobster-pgx"

func init() {
	sql.Register(postgresCompatDriverName, &postgresCompatDriver{inner: &stdlib.Driver{}})
}

type postgresCompatDriver struct {
	inner driver.Driver
}

func (d *postgresCompatDriver) Open(name string) (driver.Conn, error) {
	c, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &postgresCompatConn{Conn: c}, nil
}

type postgresCompatConn struct {
	driver.Conn
}

func (c *postgresCompatConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(translateSQL(query))
	if err != nil {
		return nil, err
	}
	return stmt, nil
}

func (c *postgresCompatConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if execer, ok := c.Conn.(driver.ExecerContext); ok {
		return execer.ExecContext(ctx, translateSQL(query), args)
	}
	return nil, driver.ErrSkip
}

func (c *postgresCompatConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if queryer, ok := c.Conn.(driver.QueryerContext); ok {
		return queryer.QueryContext(ctx, translateSQL(query), args)
	}
	return nil, driver.ErrSkip
}

func (c *postgresCompatConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if beginner, ok := c.Conn.(driver.ConnBeginTx); ok {
		return beginner.BeginTx(ctx, opts)
	}
	return c.Conn.Begin()
}

func (c *postgresCompatConn) Ping(ctx context.Context) error {
	if pinger, ok := c.Conn.(driver.Pinger); ok {
		return pinger.Ping(ctx)
	}
	return nil
}

func (c *postgresCompatConn) CheckNamedValue(v *driver.NamedValue) error {
	if checker, ok := c.Conn.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(v)
	}
	return driver.ErrSkip
}

func (c *postgresCompatConn) ResetSession(ctx context.Context) error {
	if resetter, ok := c.Conn.(driver.SessionResetter); ok {
		return resetter.ResetSession(ctx)
	}
	return nil
}

func (c *postgresCompatConn) IsValid() bool {
	if validator, ok := c.Conn.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}

func translateSQL(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return query
	}
	return replaceQuestionPlaceholders(q)
}

func replaceQuestionPlaceholders(q string) string {
	var b strings.Builder
	b.Grow(len(q) + 8)
	inSingle := false
	inDouble := false
	inLineComment := false
	inBlockComment := false
	arg := 1
	for i := 0; i < len(q); i++ {
		ch := q[i]
		next := byte(0)
		if i+1 < len(q) {
			next = q[i+1]
		}
		if inLineComment {
			b.WriteByte(ch)
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			b.WriteByte(ch)
			if ch == '*' && next == '/' {
				b.WriteByte(next)
				i++
				inBlockComment = false
			}
			continue
		}
		if !inSingle && !inDouble && ch == '-' && next == '-' {
			b.WriteByte(ch)
			b.WriteByte(next)
			i++
			inLineComment = true
			continue
		}
		if !inSingle && !inDouble && ch == '/' && next == '*' {
			b.WriteByte(ch)
			b.WriteByte(next)
			i++
			inBlockComment = true
			continue
		}
		if ch == '\'' && !inDouble {
			b.WriteByte(ch)
			if inSingle && next == '\'' {
				b.WriteByte(next)
				i++
				continue
			}
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			b.WriteByte(ch)
			inDouble = !inDouble
			continue
		}
		if ch == '?' && !inSingle && !inDouble {
			if next == '?' || (next != 0 && unicode.IsLetter(rune(next))) {
				b.WriteByte(ch)
				continue
			}
			b.WriteString(fmt.Sprintf("$%d", arg))
			arg++
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

var _ driver.Driver = (*postgresCompatDriver)(nil)
var _ driver.ExecerContext = (*postgresCompatConn)(nil)
var _ driver.QueryerContext = (*postgresCompatConn)(nil)
var _ driver.ConnBeginTx = (*postgresCompatConn)(nil)
var _ driver.Pinger = (*postgresCompatConn)(nil)
var _ driver.NamedValueChecker = (*postgresCompatConn)(nil)
var _ driver.SessionResetter = (*postgresCompatConn)(nil)
var _ driver.Validator = (*postgresCompatConn)(nil)

type translatedRows struct{ driver.Rows }

func (r translatedRows) Next(dest []driver.Value) error {
	if r.Rows == nil {
		return io.EOF
	}
	return r.Rows.Next(dest)
}
