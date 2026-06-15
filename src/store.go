// store.go — Store 接口、SQLStore 实现（v4.2 存储抽象层）
// lobster-guard v4.2 高可用
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"sync"
	"time"
)

// ============================================================
// Store 接口 — 存储抽象层（v4.2）
// 为未来 PostgresStore 等多后端做准备
// ============================================================

// AuditEntry 审计日志条目
type AuditEntry struct {
	ID              int     `json:"id"`
	Timestamp       string  `json:"timestamp"`
	Direction       string  `json:"direction"`
	SenderID        string  `json:"sender_id"`
	Action          string  `json:"action"`
	Reason          string  `json:"reason"`
	ContentPreview  string  `json:"content_preview"`
	FullRequestHash string  `json:"full_request_hash"`
	LatencyMs       float64 `json:"latency_ms"`
	UpstreamID      string  `json:"upstream_id"`
	AppID           string  `json:"app_id"`
	TraceID         string  `json:"trace_id,omitempty"` // v32.1: batch 写入需要
}

// AuditFilter 审计日志查询过滤条件
type AuditFilter struct {
	Direction string
	Action    string
	SenderID  string
	AppID     string
	Query     string // 全文搜索
	Limit     int
}

// AuditStatsResult 审计统计结果
type AuditStatsResult struct {
	Total     int            `json:"total"`
	Earliest  *string        `json:"earliest"`
	Latest    *string        `json:"latest"`
	DiskBytes int64          `json:"disk_bytes"`
	Breakdown map[string]int `json:"breakdown,omitempty"`
}

// TimelineBucket 时间线聚合桶
type TimelineBucket struct {
	Hour   string         `json:"hour"`
	Counts map[string]int `json:"counts"` // action -> count
}

// Store 存储接口 — 抽象所有数据库操作
type Store interface {
	// 审计
	LogAudit(entry *AuditEntry) error
	QueryAuditLogs(filter AuditFilter) ([]AuditEntry, error)
	CleanupAuditLogs(retentionDays int) (int, error)
	AuditStats() (*AuditStatsResult, error)
	AuditTimeline(hours int) ([]TimelineBucket, error)

	// 路由
	SaveRoute(senderID, appID, upstreamID string) error
	SaveRouteWithMeta(senderID, appID, upstreamID, department, displayName, email string) error
	DeleteRoute(senderID, appID string) error
	LoadRoutes() ([]RouteEntry, error)
	ListRoutesByApp(appID string) ([]RouteEntry, error)
	ListRoutesByDepartment(department string) ([]RouteEntry, error)
	UpdateRouteUserInfo(senderID, displayName, email, department string) error
	MigrateRoute(senderID, appID, toUpstreamID string) error
	RouteStats() (map[string]int, error) // department -> count

	// 用户信息缓存
	GetUserInfo(senderID string) (*UserInfo, error)
	SaveUserInfo(info *UserInfo) error
	ListUserInfo(department, email string) ([]*UserInfo, error)

	// 上游管理
	SaveUpstream(up *Upstream) error
	LoadUpstreams() ([]*Upstream, error)
	DeleteUpstream(id string) error

	// 生命周期
	Close() error
	Ping() error

	// 原始 DB（向后兼容 — 过渡期用）
	RawDB() *sql.DB
}

// ============================================================
// SQLStore — PostgreSQL-backed SQL 实现
// ============================================================

type SQLStore struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewSQLStore 创建 SQLStore（使用已初始化的 *sql.DB）
func NewSQLStore(db *sql.DB) *SQLStore {
	return &SQLStore{db: db}
}

func (s *SQLStore) RawDB() *sql.DB {
	return s.db
}

func (s *SQLStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *SQLStore) Ping() error {
	// 执行简单的读写验证
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS _health_check (id INTEGER PRIMARY KEY, ts TEXT)`)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(`INSERT INTO _health_check (id, ts) VALUES (1, ?) ON CONFLICT(id) DO UPDATE SET ts=EXCLUDED.ts`, now)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM _health_check WHERE id = 1`)
	return err
}

// ============================================================
// 审计日志操作
// ============================================================

func (s *SQLStore) LogAudit(entry *AuditEntry) error {
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	preview := entry.ContentPreview
	if rs := []rune(preview); len(rs) > 200 {
		preview = string(rs[:200]) + "..."
	}
	_, err := s.db.Exec(`INSERT INTO audit_log
		(timestamp,direction,sender_id,action,reason,content_preview,full_request_hash,latency_ms,upstream_id,app_id)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		entry.Timestamp, entry.Direction, entry.SenderID, entry.Action,
		entry.Reason, preview, entry.FullRequestHash, entry.LatencyMs,
		entry.UpstreamID, entry.AppID)
	return err
}

func (s *SQLStore) QueryAuditLogs(filter AuditFilter) ([]AuditEntry, error) {
	query := `SELECT id, timestamp, direction, sender_id, action, reason, content_preview, latency_ms, upstream_id, app_id FROM audit_log WHERE 1=1`
	var args []interface{}
	if filter.Direction != "" {
		query += ` AND direction=?`
		args = append(args, filter.Direction)
	}
	if filter.Action != "" {
		query += ` AND action=?`
		args = append(args, filter.Action)
	}
	if filter.SenderID != "" {
		query += ` AND sender_id=?`
		args = append(args, filter.SenderID)
	}
	if filter.AppID != "" {
		query += ` AND app_id=?`
		args = append(args, filter.AppID)
	}
	if filter.Query != "" {
		query += ` AND content_preview LIKE ?`
		args = append(args, "%"+filter.Query+"%")
	}
	query += ` ORDER BY id DESC`
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 10000 {
		limit = 10000
	}
	query += ` LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if rows.Scan(&e.ID, &e.Timestamp, &e.Direction, &e.SenderID, &e.Action, &e.Reason, &e.ContentPreview, &e.LatencyMs, &e.UpstreamID, &e.AppID) != nil {
			continue
		}
		results = append(results, e)
	}
	return results, nil
}

func (s *SQLStore) CleanupAuditLogs(retentionDays int) (int, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays).Format(time.RFC3339)
	result, err := s.db.Exec(`DELETE FROM audit_log WHERE timestamp < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	affected, _ := result.RowsAffected()
	return int(affected), nil
}

func (s *SQLStore) AuditStats() (*AuditStatsResult, error) {
	stats := &AuditStatsResult{}
	s.db.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&stats.Total)

	var earliest, latest sql.NullString
	s.db.QueryRow(`SELECT MIN(timestamp) FROM audit_log`).Scan(&earliest)
	s.db.QueryRow(`SELECT MAX(timestamp) FROM audit_log`).Scan(&latest)
	if earliest.Valid {
		stats.Earliest = &earliest.String
	}
	if latest.Valid {
		stats.Latest = &latest.String
	}

	_ = s.db.QueryRow(`SELECT pg_database_size(current_database())`).Scan(&stats.DiskBytes)

	// Breakdown
	rows, err := s.db.Query(`SELECT direction, action, COUNT(*) FROM audit_log GROUP BY direction, action`)
	if err == nil {
		defer rows.Close()
		stats.Breakdown = make(map[string]int)
		for rows.Next() {
			var dir, action string
			var cnt int
			if rows.Scan(&dir, &action, &cnt) == nil {
				stats.Breakdown[dir+"_"+action] = cnt
			}
		}
	}

	return stats, nil
}

func (s *SQLStore) AuditTimeline(hours int) ([]TimelineBucket, error) {
	if hours <= 0 {
		hours = 24
	}
	if hours > 168 {
		hours = 168
	}
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	rows, err := s.db.Query(`
		SELECT
			strftime('%Y-%m-%dT%H:00:00Z', timestamp) as hour_bucket,
			action,
			COUNT(*) as cnt
		FROM audit_log
		WHERE timestamp >= ?
		GROUP BY hour_bucket, action
		ORDER BY hour_bucket ASC
	`, since.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hourMap := map[string]map[string]int{}
	for rows.Next() {
		var hour, action string
		var count int
		if rows.Scan(&hour, &action, &count) == nil {
			if hourMap[hour] == nil {
				hourMap[hour] = map[string]int{}
			}
			hourMap[hour][action] = count
		}
	}

	var timeline []TimelineBucket
	for i := hours - 1; i >= 0; i-- {
		t := time.Now().UTC().Add(-time.Duration(i) * time.Hour)
		hourKey := t.Format("2006-01-02T15") + ":00:00Z"
		bucket := TimelineBucket{
			Hour:   hourKey,
			Counts: map[string]int{"pass": 0, "block": 0, "warn": 0},
		}
		if m, ok := hourMap[hourKey]; ok {
			for action, cnt := range m {
				bucket.Counts[action] = cnt
			}
		}
		timeline = append(timeline, bucket)
	}
	return timeline, nil
}

// ============================================================
// 路由操作
// ============================================================

func (s *SQLStore) SaveRoute(senderID, appID, upstreamID string) error {
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.Exec(`INSERT OR REPLACE INTO user_routes (sender_id, app_id, upstream_id, department, display_name, email, created_at, updated_at) VALUES(?,?,?,'','','',?,?)`,
		senderID, appID, upstreamID, now, now)
	return err
}

func (s *SQLStore) SaveRouteWithMeta(senderID, appID, upstreamID, department, displayName, email string) error {
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.Exec(`INSERT OR REPLACE INTO user_routes (sender_id, app_id, upstream_id, department, display_name, email, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		senderID, appID, upstreamID, department, displayName, email, now, now)
	return err
}

func (s *SQLStore) DeleteRoute(senderID, appID string) error {
	_, err := s.db.Exec(`DELETE FROM user_routes WHERE sender_id = ? AND app_id = ?`, senderID, appID)
	return err
}

func (s *SQLStore) LoadRoutes() ([]RouteEntry, error) {
	rows, err := s.db.Query(`SELECT sender_id, app_id, upstream_id, department, display_name, COALESCE(email,''), created_at, updated_at FROM user_routes ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []RouteEntry
	for rows.Next() {
		var e RouteEntry
		if rows.Scan(&e.SenderID, &e.AppID, &e.UpstreamID, &e.Department, &e.DisplayName, &e.Email, &e.CreatedAt, &e.UpdatedAt) == nil {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

func (s *SQLStore) ListRoutesByApp(appID string) ([]RouteEntry, error) {
	rows, err := s.db.Query(`SELECT sender_id, app_id, upstream_id, department, display_name, COALESCE(email,''), created_at, updated_at FROM user_routes WHERE app_id = ? ORDER BY updated_at DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []RouteEntry
	for rows.Next() {
		var e RouteEntry
		if rows.Scan(&e.SenderID, &e.AppID, &e.UpstreamID, &e.Department, &e.DisplayName, &e.Email, &e.CreatedAt, &e.UpdatedAt) == nil {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

func (s *SQLStore) ListRoutesByDepartment(department string) ([]RouteEntry, error) {
	rows, err := s.db.Query(`SELECT sender_id, app_id, upstream_id, department, display_name, COALESCE(email,''), created_at, updated_at FROM user_routes WHERE department = ? ORDER BY updated_at DESC`, department)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []RouteEntry
	for rows.Next() {
		var e RouteEntry
		if rows.Scan(&e.SenderID, &e.AppID, &e.UpstreamID, &e.Department, &e.DisplayName, &e.Email, &e.CreatedAt, &e.UpdatedAt) == nil {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

func (s *SQLStore) UpdateRouteUserInfo(senderID, displayName, email, department string) error {
	now := time.Now().Format(time.RFC3339)
	s.db.Exec(`UPDATE user_routes SET display_name=?, department=?, updated_at=? WHERE sender_id=? AND (display_name='' OR display_name IS NULL OR display_name!=?)`,
		displayName, department, now, senderID, displayName)
	s.db.Exec(`UPDATE user_routes SET email=?, updated_at=? WHERE sender_id=? AND (email='' OR email IS NULL OR email!=?)`,
		email, now, senderID, email)
	return nil
}

func (s *SQLStore) MigrateRoute(senderID, appID, toUpstreamID string) error {
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.Exec(`UPDATE user_routes SET upstream_id=?, updated_at=? WHERE sender_id=? AND app_id=?`,
		toUpstreamID, now, senderID, appID)
	return err
}

func (s *SQLStore) RouteStats() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT COALESCE(department,''), COUNT(*) FROM user_routes GROUP BY department`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int)
	for rows.Next() {
		var dept string
		var cnt int
		if rows.Scan(&dept, &cnt) == nil && dept != "" {
			result[dept] = cnt
		}
	}
	return result, nil
}

// ============================================================
// 用户信息缓存操作
// ============================================================

func (s *SQLStore) GetUserInfo(senderID string) (*UserInfo, error) {
	var info UserInfo
	var fetchedAt string
	err := s.db.QueryRow(`SELECT sender_id, name, email, department, avatar, mobile, fetched_at FROM user_info_cache WHERE sender_id = ?`, senderID).
		Scan(&info.SenderID, &info.Name, &info.Email, &info.Department, &info.Avatar, &info.Mobile, &fetchedAt)
	if err != nil {
		return nil, err
	}
	t, _ := time.Parse(time.RFC3339, fetchedAt)
	info.FetchedAt = t
	return &info, nil
}

func (s *SQLStore) SaveUserInfo(info *UserInfo) error {
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.Exec(`INSERT OR REPLACE INTO user_info_cache (sender_id, name, email, department, avatar, mobile, fetched_at, updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		info.SenderID, info.Name, info.Email, info.Department, info.Avatar, info.Mobile, info.FetchedAt.Format(time.RFC3339), now)
	return err
}

func (s *SQLStore) ListUserInfo(department, email string) ([]*UserInfo, error) {
	query := `SELECT sender_id, name, email, department, avatar, mobile, fetched_at FROM user_info_cache WHERE 1=1`
	var args []interface{}
	if department != "" {
		query += ` AND department = ?`
		args = append(args, department)
	}
	if email != "" {
		query += ` AND email LIKE ?`
		args = append(args, "%"+email+"%")
	}
	query += ` ORDER BY updated_at DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []*UserInfo
	for rows.Next() {
		var info UserInfo
		var fetchedAt string
		if rows.Scan(&info.SenderID, &info.Name, &info.Email, &info.Department, &info.Avatar, &info.Mobile, &fetchedAt) == nil {
			t, _ := time.Parse(time.RFC3339, fetchedAt)
			info.FetchedAt = t
			results = append(results, &info)
		}
	}
	return results, nil
}

// ============================================================
// 上游管理操作
// ============================================================

func (s *SQLStore) SaveUpstream(up *Upstream) error {
	tagsJSON := "{}"
	loadJSON := "{}"
	if up.Tags != nil {
		if b, err := jsonMarshalSafe(up.Tags); err == nil {
			tagsJSON = string(b)
		}
	}
	if up.Load != nil {
		if b, err := jsonMarshalSafe(up.Load); err == nil {
			loadJSON = string(b)
		}
	}
	h := 0
	if up.Healthy {
		h = 1
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO upstreams (id,address,port,healthy,registered_at,last_heartbeat,tags,load) VALUES(?,?,?,?,?,?,?,?)`,
		up.ID, up.Address, up.Port, h, up.RegisteredAt.Format(time.RFC3339), up.LastHeartbeat.Format(time.RFC3339),
		tagsJSON, loadJSON)
	return err
}

func (s *SQLStore) LoadUpstreams() ([]*Upstream, error) {
	rows, err := s.db.Query(`SELECT id, address, port, healthy, registered_at, last_heartbeat, tags, load FROM upstreams`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []*Upstream
	for rows.Next() {
		var id, address, regAt, hbAt, tagsJSON, loadJSON string
		var port, healthy int
		if rows.Scan(&id, &address, &port, &healthy, &regAt, &hbAt, &tagsJSON, &loadJSON) != nil {
			continue
		}
		up := &Upstream{
			ID: id, Address: address, Port: port, Healthy: healthy == 1,
			Tags: map[string]string{}, Load: map[string]interface{}{},
		}
		up.RegisteredAt, _ = time.Parse(time.RFC3339, regAt)
		up.LastHeartbeat, _ = time.Parse(time.RFC3339, hbAt)
		jsonUnmarshalSafe([]byte(tagsJSON), &up.Tags)
		jsonUnmarshalSafe([]byte(loadJSON), &up.Load)
		results = append(results, up)
	}
	return results, nil
}

func (s *SQLStore) DeleteUpstream(id string) error {
	_, err := s.db.Exec(`DELETE FROM upstreams WHERE id = ?`, id)
	return err
}

// ============================================================
// 备份操作（v4.2）
// ============================================================

func (s *SQLStore) Backup(backupDir string) (string, int64, error) {
	return "", 0, fmt.Errorf("PostgreSQL 文件级备份不由应用内置执行，请使用 pg_dump/pg_restore")
}

// ListBackups 列出备份目录中的所有备份
func ListBackups(backupDir string) ([]BackupInfo, error) {
	return []BackupInfo{}, nil
}

// CleanupOldBackups 删除超过 maxCount 的旧备份
func CleanupOldBackups(backupDir string, maxCount int) (int, error) {
	return 0, nil
}

// DeleteBackup 删除指定备份文件
func DeleteBackup(backupDir, name string) error {
	return fmt.Errorf("PostgreSQL 文件级备份不由应用内置管理，请使用 pg_dump/pg_restore")
}

// RestoreFromBackup 从备份文件恢复
func RestoreFromBackup(backupPath, targetPath string) error {
	return fmt.Errorf("PostgreSQL 恢复不由应用内置执行，请使用 pg_restore")
}

// BackupInfo 备份文件信息
type BackupInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"created_at"`
}

// jsonMarshalSafe wraps json.Marshal for store usage
func jsonMarshalSafe(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// jsonUnmarshalSafe wraps json.Unmarshal for store usage
func jsonUnmarshalSafe(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
