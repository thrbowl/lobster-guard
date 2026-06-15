package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"regexp"
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
	upper := strings.ToUpper(q)
	if strings.HasPrefix(upper, "PRAGMA ") {
		return "SELECT 0"
	}
	if strings.Contains(upper, "FROM SQLITE_MASTER") {
		q = strings.ReplaceAll(q, "sqlite_master", "information_schema.tables")
		q = strings.ReplaceAll(q, "type='table'", "table_type='BASE TABLE'")
		q = strings.ReplaceAll(q, "type = 'table'", "table_type = 'BASE TABLE'")
		q = strings.ReplaceAll(q, "name", "table_name")
	}
	q = rewriteSQLiteSchema(q)
	q = rewriteSQLiteFunctions(q)
	q = rewriteInsertOrReplace(q)
	q = replaceRowID(q)
	return replaceQuestionPlaceholders(q)
}

func rewriteSQLiteSchema(q string) string {
	repls := []struct{ old, new string }{
		{"INTEGER PRIMARY KEY AUTOINCREMENT", "BIGSERIAL PRIMARY KEY"},
		{"integer primary key autoincrement", "BIGSERIAL PRIMARY KEY"},
		{"INTEGER PRIMARY KEY", "BIGSERIAL PRIMARY KEY"},
		{"integer primary key", "BIGSERIAL PRIMARY KEY"},
		{"BOOLEAN DEFAULT 0", "BOOLEAN DEFAULT FALSE"},
		{"BOOLEAN DEFAULT 1", "BOOLEAN DEFAULT TRUE"},
	}
	for _, r := range repls {
		q = strings.ReplaceAll(q, r.old, r.new)
	}
	return q
}

func rewriteSQLiteFunctions(q string) string {
	q = strings.ReplaceAll(q, "CAST(strftime('%H', timestamp) AS INTEGER)", "EXTRACT(HOUR FROM timestamp::timestamp)::int")
	q = strings.ReplaceAll(q, `strftime('%Y-%m-%dT%H:00:00Z', timestamp)`, `to_char(date_trunc('hour', timestamp::timestamp), 'YYYY-MM-DD"T"HH24:00:00"Z"')`)
	q = strings.ReplaceAll(q, "date(timestamp)", "DATE(timestamp::timestamp)")
	q = strings.ReplaceAll(q, "date('now', '-7 days')", "(CURRENT_DATE - INTERVAL '7 days')")
	return q
}

func replaceRowID(q string) string {
	re := regexp.MustCompile(`(?i)\browid\b`)
	return re.ReplaceAllString(q, "id")
}

func rewriteInsertOrReplace(q string) string {
	trimmed := strings.TrimSpace(q)
	if !strings.HasPrefix(strings.ToUpper(trimmed), "INSERT OR REPLACE INTO ") {
		return q
	}
	re := regexp.MustCompile(`(?is)^INSERT\s+OR\s+REPLACE\s+INTO\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\(([^)]*)\)\s*VALUES\s*\((.*)\)\s*$`)
	m := re.FindStringSubmatch(trimmed)
	if len(m) != 4 {
		return strings.Replace(trimmed, "INSERT OR REPLACE INTO", "INSERT INTO", 1)
	}
	table := m[1]
	cols := splitColumns(m[2])
	if len(cols) == 0 {
		return strings.Replace(trimmed, "INSERT OR REPLACE INTO", "INSERT INTO", 1)
	}
	conflict := conflictColumnsFor(table, cols)
	if len(conflict) == 0 {
		conflict = []string{cols[0]}
	}
	setParts := make([]string, 0, len(cols))
	for _, col := range cols {
		if pgCompatContainsString(conflict, col) {
			continue
		}
		setParts = append(setParts, fmt.Sprintf("%s=EXCLUDED.%s", col, col))
	}
	if len(setParts) == 0 {
		return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO NOTHING", table, strings.Join(cols, ","), m[3], strings.Join(conflict, ","))
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s", table, strings.Join(cols, ","), m[3], strings.Join(conflict, ","), strings.Join(setParts, ","))
}

func splitColumns(s string) []string {
	parts := strings.Split(s, ",")
	cols := make([]string, 0, len(parts))
	for _, p := range parts {
		col := strings.TrimSpace(strings.Trim(p, `"`))
		if col != "" {
			cols = append(cols, col)
		}
	}
	return cols
}

func conflictColumnsFor(table string, cols []string) []string {
	switch table {
	case "user_routes":
		if pgCompatContainsString(cols, "sender_id") && pgCompatContainsString(cols, "app_id") {
			return []string{"sender_id", "app_id"}
		}
	case "upstreams":
		return []string{"id"}
	case "user_info_cache":
		return []string{"sender_id"}
	case "tenant_inbound_rules", "tenant_llm_rules":
		return []string{"tenant_id"}
	case "ifc_source_rules":
		return []string{"source"}
	case "ifc_tool_requirements":
		return []string{"tool"}
	case "ifc_hidden_content":
		return []string{"var_id"}
	case "llm_cache":
		return []string{"key"}
	case "prompt_versions":
		return []string{"hash"}
	case "ab_tests", "attack_chains", "redteam_reports", "taint_entries", "taint_custom_rules":
		return []string{"id"}
	}
	if pgCompatContainsString(cols, "id") {
		return []string{"id"}
	}
	return nil
}

func pgCompatContainsString(values []string, needle string) bool {
	for _, v := range values {
		if v == needle {
			return true
		}
	}
	return false
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
