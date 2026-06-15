package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
)

func openTestPostgres(t *testing.T) *sql.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("LOBSTER_GUARD_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("LOBSTER_GUARD_TEST_DATABASE_URL 未配置，跳过 PostgreSQL 集成测试")
	}
	schema := sanitizeTestSchemaName(fmt.Sprintf("test_%s", t.Name()))
	adminDB, err := sql.Open(postgresCompatDriverName, dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if _, err := adminDB.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`); err != nil {
		_ = adminDB.Close()
		t.Fatalf("drop test schema: %v", err)
	}
	if _, err := adminDB.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create test schema: %v", err)
	}
	_ = adminDB.Close()

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, err := sql.Open(postgresCompatDriverName, dsn+sep+"search_path="+schema)
	if err != nil {
		t.Fatalf("open postgres test schema: %v", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatalf("ping postgres test schema: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		cleanupDB, err := sql.Open(postgresCompatDriverName, dsn)
		if err == nil {
			_, _ = cleanupDB.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`)
			_ = cleanupDB.Close()
		}
	})
	return db
}

func openTestSQLite(t *testing.T) *sql.DB {
	t.Helper()
	return openTestPostgres(t)
}

func openTestSQLiteCompat(t *testing.T) *sql.DB {
	t.Helper()
	return openTestPostgres(t)
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	return openTestPostgres(t)
}

func sanitizeTestSchemaName(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "test_schema"
	}
	if len(out) > 55 {
		out = out[:55]
	}
	return out
}
