package main

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func initPostgres(databaseURL string) (*sql.DB, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, fmt.Errorf("database_url 未配置")
	}
	db, err := sql.Open(postgresCompatDriverName, databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("连接 PostgreSQL 失败: %w", err)
	}
	if err := migratePostgres(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migratePostgres(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS audit_log (
	id BIGSERIAL PRIMARY KEY,
	timestamp TEXT NOT NULL,
	direction TEXT NOT NULL,
	sender_id TEXT,
	action TEXT NOT NULL,
	reason TEXT,
	content_preview TEXT,
	full_request_hash TEXT,
	latency_ms DOUBLE PRECISION,
	upstream_id TEXT DEFAULT '',
	app_id TEXT DEFAULT '',
	trace_id TEXT DEFAULT '',
	tenant_id TEXT DEFAULT 'default'
);
CREATE INDEX IF NOT EXISTS idx_ts ON audit_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_dir ON audit_log(direction);
CREATE INDEX IF NOT EXISTS idx_act ON audit_log(action);
CREATE INDEX IF NOT EXISTS idx_sender ON audit_log(sender_id);
CREATE INDEX IF NOT EXISTS idx_trace ON audit_log(trace_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_tenant ON audit_log(tenant_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_tenant_action_ts ON audit_log(tenant_id, action, timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_log_sender_ts ON audit_log(sender_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_log_action_ts ON audit_log(action, timestamp);

CREATE TABLE IF NOT EXISTS upstreams (
	id TEXT PRIMARY KEY,
	address TEXT NOT NULL,
	port INTEGER NOT NULL,
	healthy INTEGER DEFAULT 1,
	registered_at TEXT NOT NULL,
	last_heartbeat TEXT,
	tags TEXT DEFAULT '{}',
	load TEXT DEFAULT '{}',
	path_prefix TEXT DEFAULT '',
	gateway_token TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS llm_calls (
	id BIGSERIAL PRIMARY KEY,
	timestamp TEXT NOT NULL,
	trace_id TEXT,
	model TEXT,
	request_tokens INTEGER,
	response_tokens INTEGER,
	total_tokens INTEGER,
	latency_ms DOUBLE PRECISION,
	status_code INTEGER,
	has_tool_use INTEGER DEFAULT 0,
	tool_count INTEGER DEFAULT 0,
	error_message TEXT,
	tenant_id TEXT DEFAULT 'default',
	prompt_hash TEXT DEFAULT '',
	error_type TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_llm_calls_ts ON llm_calls(timestamp);
CREATE INDEX IF NOT EXISTS idx_llm_calls_trace ON llm_calls(trace_id);
CREATE INDEX IF NOT EXISTS idx_llm_calls_tenant ON llm_calls(tenant_id);
CREATE INDEX IF NOT EXISTS idx_llm_calls_prompt_hash ON llm_calls(prompt_hash);
CREATE INDEX IF NOT EXISTS idx_llm_calls_model ON llm_calls(model);
CREATE INDEX IF NOT EXISTS idx_llm_calls_error_type ON llm_calls(error_type);

CREATE TABLE IF NOT EXISTS llm_tool_calls (
	id BIGSERIAL PRIMARY KEY,
	llm_call_id BIGINT REFERENCES llm_calls(id),
	timestamp TEXT NOT NULL,
	tool_name TEXT NOT NULL,
	tool_input_preview TEXT,
	tool_result_preview TEXT,
	risk_level TEXT DEFAULT 'low',
	flagged INTEGER DEFAULT 0,
	flag_reason TEXT,
	tenant_id TEXT DEFAULT 'default',
	source_key TEXT DEFAULT '',
	source_category TEXT DEFAULT '',
	source_descriptor_json TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_llm_tool_calls_ts ON llm_tool_calls(timestamp);
CREATE INDEX IF NOT EXISTS idx_llm_tool_calls_risk ON llm_tool_calls(risk_level);
CREATE INDEX IF NOT EXISTS idx_llm_tool_calls_tool ON llm_tool_calls(tool_name);
CREATE INDEX IF NOT EXISTS idx_llm_tool_calls_call_id ON llm_tool_calls(llm_call_id);
CREATE INDEX IF NOT EXISTS idx_llm_tool_calls_flagged_ts ON llm_tool_calls(flagged, timestamp);
CREATE INDEX IF NOT EXISTS idx_llm_tool_calls_risk_ts ON llm_tool_calls(risk_level, timestamp);
CREATE INDEX IF NOT EXISTS idx_llm_tool_calls_tenant ON llm_tool_calls(tenant_id);
CREATE INDEX IF NOT EXISTS idx_llm_tool_calls_source_category ON llm_tool_calls(source_category);

CREATE TABLE IF NOT EXISTS user_routes (
	sender_id TEXT NOT NULL,
	app_id TEXT NOT NULL DEFAULT '',
	upstream_id TEXT NOT NULL,
	department TEXT DEFAULT '',
	display_name TEXT DEFAULT '',
	email TEXT DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (sender_id, app_id)
);
CREATE INDEX IF NOT EXISTS idx_routes_upstream ON user_routes(upstream_id);
CREATE INDEX IF NOT EXISTS idx_routes_app ON user_routes(app_id);
CREATE INDEX IF NOT EXISTS idx_routes_dept ON user_routes(department);
CREATE INDEX IF NOT EXISTS idx_routes_email ON user_routes(email);

CREATE TABLE IF NOT EXISTS user_info_cache (
	sender_id TEXT PRIMARY KEY,
	name TEXT DEFAULT '',
	email TEXT DEFAULT '',
	department TEXT DEFAULT '',
	avatar TEXT DEFAULT '',
	mobile TEXT DEFAULT '',
	fetched_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_user_email ON user_info_cache(email);
CREATE INDEX IF NOT EXISTS idx_user_dept ON user_info_cache(department);
`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("初始化 PostgreSQL schema 失败: %w", err)
	}
	ensurePostgresColumns(db)
	return nil
}

func ensurePostgresColumns(db *sql.DB) {
	stmts := []string{
		`ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS upstream_id TEXT DEFAULT ''`,
		`ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS app_id TEXT DEFAULT ''`,
		`ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS trace_id TEXT DEFAULT ''`,
		`ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS tenant_id TEXT DEFAULT 'default'`,
		`ALTER TABLE upstreams ADD COLUMN IF NOT EXISTS path_prefix TEXT DEFAULT ''`,
		`ALTER TABLE upstreams ADD COLUMN IF NOT EXISTS gateway_token TEXT DEFAULT ''`,
		`ALTER TABLE user_routes ADD COLUMN IF NOT EXISTS email TEXT DEFAULT ''`,
		`ALTER TABLE llm_calls ADD COLUMN IF NOT EXISTS tenant_id TEXT DEFAULT 'default'`,
		`ALTER TABLE llm_calls ADD COLUMN IF NOT EXISTS prompt_hash TEXT DEFAULT ''`,
		`ALTER TABLE llm_calls ADD COLUMN IF NOT EXISTS error_type TEXT DEFAULT ''`,
		`ALTER TABLE llm_tool_calls ADD COLUMN IF NOT EXISTS tenant_id TEXT DEFAULT 'default'`,
		`ALTER TABLE llm_tool_calls ADD COLUMN IF NOT EXISTS source_key TEXT DEFAULT ''`,
		`ALTER TABLE llm_tool_calls ADD COLUMN IF NOT EXISTS source_category TEXT DEFAULT ''`,
		`ALTER TABLE llm_tool_calls ADD COLUMN IF NOT EXISTS source_descriptor_json TEXT DEFAULT ''`,
	}
	for _, stmt := range stmts {
		_, _ = db.Exec(stmt)
	}
}

func maskDatabaseURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	username := u.User.Username()
	if username == "" {
		u.User = nil
	} else {
		u.User = url.UserPassword(username, "****")
	}
	return u.String()
}
