package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// RoutePolicyStore stores route policies in the database. Policies are ordered
// by position and matched with the stateless MatchRoutePolicy helper.
type RoutePolicyStore struct {
	db *sql.DB
}

func routePolicyDBFromLogger(logger *AuditLogger) *sql.DB {
	if logger == nil {
		return nil
	}
	return logger.DB()
}

func NewRoutePolicyStore(db *sql.DB, initial []RoutePolicyConfig) *RoutePolicyStore {
	s := &RoutePolicyStore{db: db}
	if db != nil {
		s.initSchema()
		s.seedIfEmpty(initial)
	}
	return s
}

func (s *RoutePolicyStore) initSchema() {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS route_policies (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		position INTEGER NOT NULL,
		policy_json TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		log.Printf("[策略路由] 初始化 route_policies 失败: %v", err)
		return
	}
	_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_route_policies_position ON route_policies(position)`)
}

func (s *RoutePolicyStore) seedIfEmpty(initial []RoutePolicyConfig) {
	if len(initial) == 0 {
		return
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM route_policies`).Scan(&count); err != nil || count > 0 {
		return
	}
	if err := s.ReplaceAll(initial); err != nil {
		log.Printf("[策略路由] 初始策略写入数据库失败: %v", err)
	}
}

func (s *RoutePolicyStore) List() ([]RoutePolicyConfig, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT policy_json FROM route_policies ORDER BY position ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	policies := []RoutePolicyConfig{}
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) != nil {
			continue
		}
		var p RoutePolicyConfig
		if json.Unmarshal([]byte(raw), &p) == nil {
			policies = append(policies, p)
		}
	}
	return policies, rows.Err()
}

func (s *RoutePolicyStore) ReplaceAll(policies []RoutePolicyConfig) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("route policy store not initialized")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM route_policies`); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for i, p := range policies {
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO route_policies (position, policy_json, created_at, updated_at) VALUES (?,?,?,?)`, i, string(raw), now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *RoutePolicyStore) Match(info *UserInfo, appID string) (int, *RoutePolicyConfig, bool, error) {
	policies, err := s.List()
	if err != nil {
		return -1, nil, false, err
	}
	idx, p, ok := MatchRoutePolicy(policies, info, appID)
	return idx, p, ok, nil
}

func validateRoutePolicy(p RoutePolicyConfig, allowDefaultEmpty bool) error {
	hasFixedResponse := p.FixedResponse != nil && p.FixedResponse.Enabled
	if p.UpstreamID == "" && !hasFixedResponse && !(allowDefaultEmpty && p.Match.Default) {
		return fmt.Errorf("upstream_id is required (unless fixed_response is enabled)")
	}
	if len(p.UpstreamID) > 256 {
		return fmt.Errorf("upstream_id must be <= 256 characters")
	}
	if !p.Match.Default && p.Match.Department == "" && p.Match.EmailSuffix == "" && p.Match.Email == "" && p.Match.AppID == "" {
		return fmt.Errorf("match conditions cannot be empty, set at least one field or use default:true")
	}
	return nil
}

func samePolicyMatch(a, b RoutePolicyConfig) bool {
	return a.Match.Department == b.Match.Department &&
		a.Match.EmailSuffix == b.Match.EmailSuffix &&
		a.Match.Email == b.Match.Email &&
		a.Match.AppID == b.Match.AppID &&
		a.Match.Default == b.Match.Default
}
