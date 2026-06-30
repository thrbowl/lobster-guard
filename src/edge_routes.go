package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultEdgeRoutesExportPath = "/etc/lobster-guard/edge-routes.json"
	edgeRouteModeObserve        = "observe"
	edgeRouteModeEnforce        = "enforce"
	edgeRouteHostPreserve       = "preserve"
	edgeRouteHostUpstream       = "upstream_host"
	edgeRouteMatchPrefix        = "prefix"
	edgeRouteMatchExact         = "exact"
	defaultEdgeProjectTenantID  = "default"
)

var ErrEdgeProjectInUse = errors.New("edge project is referenced by edge routes")

type EdgeProject struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	TenantID    string   `json:"tenant_id"`
	Hosts       []string `json:"hosts"`
	UpstreamURL string   `json:"upstream_url"`
	HostPolicy  string   `json:"host_policy"`
	DefaultMode string   `json:"default_mode"`
	Description string   `json:"description,omitempty"`
	Enabled     bool     `json:"enabled"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
}

type EdgeRoute struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name,omitempty"`
	PathPrefix  string `json:"path_prefix"`
	MatchType   string `json:"match_type"`
	Mode        string `json:"mode"`
	Enabled     bool   `json:"enabled"`
	Priority    int    `json:"priority"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type EdgeIngressRoute struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name,omitempty"`
	Host        string `json:"host"`
	PathPrefix  string `json:"path_prefix"`
	Mode        string `json:"mode"`
	UpstreamURL string `json:"upstream_url"`
	HostPolicy  string `json:"host_policy"`
	Enabled     bool   `json:"enabled"`
	Priority    int    `json:"priority"`
	Description string `json:"description,omitempty"`
}

type EdgeRouteExport struct {
	Version     int64              `json:"version"`
	GeneratedAt string             `json:"generated_at"`
	Checksum    string             `json:"checksum"`
	Routes      []EdgeIngressRoute `json:"routes"`
}

type EdgeRouteSyncResult struct {
	Path        string `json:"path"`
	Version     int64  `json:"version"`
	GeneratedAt string `json:"generated_at"`
	Checksum    string `json:"checksum"`
	Total       int    `json:"total"`
}

type EdgeRouteManager struct {
	db         *sql.DB
	exportPath string
	projects   *EdgeProjectManager
}

type EdgeProjectManager struct {
	db *sql.DB
}

type TapExchangeEvent struct {
	ID               int64   `json:"id,omitempty"`
	CreatedAt        string  `json:"created_at,omitempty"`
	RouteID          string  `json:"route_id"`
	ProjectID        string  `json:"project_id"`
	ProjectName      string  `json:"project_name,omitempty"`
	TenantID         string  `json:"tenant_id,omitempty"`
	Mode             string  `json:"mode"`
	TraceID          string  `json:"trace_id,omitempty"`
	Method           string  `json:"method,omitempty"`
	Host             string  `json:"host,omitempty"`
	URI              string  `json:"uri,omitempty"`
	UpstreamURL      string  `json:"upstream_url,omitempty"`
	UpstreamStatus   int     `json:"upstream_status,omitempty"`
	ResponseStatus   int     `json:"response_status,omitempty"`
	DurationMs       float64 `json:"duration_ms,omitempty"`
	Action           string  `json:"action,omitempty"`
	Reason           string  `json:"reason,omitempty"`
	RequestPreview   string  `json:"request_preview,omitempty"`
	RequestBodyHash  string  `json:"request_body_hash,omitempty"`
	ResponsePreview  string  `json:"response_preview,omitempty"`
	ResponseBodyHash string  `json:"response_body_hash,omitempty"`
}

type TapExchangeEventQuery struct {
	ProjectID string
	RouteID   string
	Mode      string
	Status    int
	Q         string
	From      string
	To        string
	Limit     int
	Offset    int
}

func NewEdgeProjectManager(db *sql.DB) (*EdgeProjectManager, error) {
	if db == nil {
		return nil, errors.New("edge project database is nil")
	}
	m := &EdgeProjectManager{db: db}
	if err := m.initSchema(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *EdgeProjectManager) initSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS edge_projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			tenant_id TEXT NOT NULL DEFAULT 'default',
			hosts_json TEXT NOT NULL DEFAULT '[]',
			upstream_url TEXT NOT NULL DEFAULT '',
			host_policy TEXT NOT NULL DEFAULT 'upstream_host',
			default_mode TEXT NOT NULL DEFAULT 'observe',
			description TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_edge_projects_tenant ON edge_projects(tenant_id)`,
		`ALTER TABLE edge_projects ADD COLUMN hosts_json TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE edge_projects ADD COLUMN upstream_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE edge_projects ADD COLUMN host_policy TEXT NOT NULL DEFAULT 'upstream_host'`,
		`ALTER TABLE edge_projects ADD COLUMN default_mode TEXT NOT NULL DEFAULT 'observe'`,
	}
	for _, stmt := range stmts {
		if _, err := m.db.Exec(stmt); err != nil {
			lowerErr := strings.ToLower(err.Error())
			if strings.Contains(lowerErr, "duplicate column") || strings.Contains(lowerErr, "already exists") {
				continue
			}
			return err
		}
	}
	return nil
}

func (m *EdgeProjectManager) ListProjects() ([]EdgeProject, error) {
	rows, err := m.db.Query(`SELECT id, name, tenant_id, hosts_json, upstream_url, host_policy, default_mode,
		description, enabled, created_at, updated_at
		FROM edge_projects ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []EdgeProject
	for rows.Next() {
		project, err := scanEdgeProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (m *EdgeProjectManager) GetProject(id string) (EdgeProject, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return EdgeProject{}, errors.New("project id required")
	}
	project, err := scanEdgeProject(m.db.QueryRow(`SELECT id, name, tenant_id, hosts_json, upstream_url, host_policy, default_mode,
		description, enabled, created_at, updated_at
		FROM edge_projects WHERE id=?`, id))
	if err != nil {
		return EdgeProject{}, err
	}
	return project, nil
}

func (m *EdgeProjectManager) CreateProject(project EdgeProject) (EdgeProject, error) {
	project = normalizeEdgeProject(project)
	if err := m.ValidateProject(project); err != nil {
		return EdgeProject{}, err
	}
	hostsJSON, err := json.Marshal(project.Hosts)
	if err != nil {
		return EdgeProject{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = m.db.Exec(`INSERT INTO edge_projects
		(id, name, tenant_id, hosts_json, upstream_url, host_policy, default_mode, description, enabled, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		project.ID, project.Name, project.TenantID, string(hostsJSON), project.UpstreamURL, project.HostPolicy,
		project.DefaultMode, project.Description, edgeBoolToInt(project.Enabled), now, now)
	if err != nil {
		return EdgeProject{}, err
	}
	project.CreatedAt = now
	project.UpdatedAt = now
	return project, nil
}

func (m *EdgeProjectManager) UpdateProject(id string, project EdgeProject) (EdgeProject, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return EdgeProject{}, errors.New("project id required")
	}
	project.ID = id
	project = normalizeEdgeProject(project)
	if err := m.ValidateProject(project); err != nil {
		return EdgeProject{}, err
	}
	hostsJSON, err := json.Marshal(project.Hosts)
	if err != nil {
		return EdgeProject{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := m.db.Exec(`UPDATE edge_projects SET
		name=?, tenant_id=?, hosts_json=?, upstream_url=?, host_policy=?, default_mode=?, description=?, enabled=?, updated_at=?
		WHERE id=?`,
		project.Name, project.TenantID, string(hostsJSON), project.UpstreamURL, project.HostPolicy, project.DefaultMode,
		project.Description, edgeBoolToInt(project.Enabled), now, project.ID)
	if err != nil {
		return EdgeProject{}, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return EdgeProject{}, sql.ErrNoRows
	}
	project.UpdatedAt = now
	return project, nil
}

func (m *EdgeProjectManager) DeleteProject(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("project id required")
	}
	var refs int
	if err := m.db.QueryRow(`SELECT COUNT(*) FROM edge_routes WHERE project_id=?`, id).Scan(&refs); err != nil {
		return err
	}
	if refs > 0 {
		return ErrEdgeProjectInUse
	}
	res, err := m.db.Exec(`DELETE FROM edge_projects WHERE id=?`, id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (m *EdgeProjectManager) ValidateProject(project EdgeProject) error {
	project = normalizeEdgeProject(project)
	if project.ID == "" {
		return errors.New("id required")
	}
	if strings.ContainsAny(project.ID, " \t\r\n/\\") {
		return errors.New("id must not contain spaces or path separators")
	}
	if project.Name == "" {
		return errors.New("name required")
	}
	if project.TenantID == "" {
		return errors.New("tenant_id required")
	}
	if project.TenantID != defaultEdgeProjectTenantID {
		if err := m.ensureTenantExists(project.TenantID); err != nil {
			return err
		}
	}
	if len(project.Hosts) == 0 {
		return errors.New("hosts required")
	}
	for _, host := range project.Hosts {
		if err := validateEdgeHost(host); err != nil {
			return err
		}
	}
	if err := validateEdgeUpstreamURL(project.UpstreamURL); err != nil {
		return err
	}
	if project.HostPolicy != edgeRouteHostPreserve && project.HostPolicy != edgeRouteHostUpstream {
		return errors.New("host_policy must be preserve or upstream_host")
	}
	if project.DefaultMode != edgeRouteModeObserve && project.DefaultMode != edgeRouteModeEnforce {
		return errors.New("default_mode must be observe or enforce")
	}
	return nil
}

func (m *EdgeProjectManager) ensureTenantExists(tenantID string) error {
	var one int
	err := m.db.QueryRow(`SELECT 1 FROM tenants WHERE id=? AND enabled != 0`, tenantID).Scan(&one)
	if err == sql.ErrNoRows {
		return fmt.Errorf("tenant_id %s not found or disabled", tenantID)
	}
	if err != nil {
		return fmt.Errorf("tenant_id %s cannot be validated: %w", tenantID, err)
	}
	return nil
}

func (m *EdgeProjectManager) ResolveEnabledProject(id string) (EdgeProject, error) {
	project, err := m.GetProject(id)
	if err != nil {
		return EdgeProject{}, err
	}
	if !project.Enabled {
		return EdgeProject{}, fmt.Errorf("project_id %s is disabled", project.ID)
	}
	return project, nil
}

type edgeProjectScanner interface {
	Scan(dest ...interface{}) error
}

func scanEdgeProject(row edgeProjectScanner) (EdgeProject, error) {
	var project EdgeProject
	var hostsJSON string
	var enabled int
	if err := row.Scan(&project.ID, &project.Name, &project.TenantID, &hostsJSON, &project.UpstreamURL,
		&project.HostPolicy, &project.DefaultMode, &project.Description, &enabled,
		&project.CreatedAt, &project.UpdatedAt); err != nil {
		return EdgeProject{}, err
	}
	if err := json.Unmarshal([]byte(hostsJSON), &project.Hosts); err != nil {
		return EdgeProject{}, err
	}
	project.Enabled = enabled != 0
	return normalizeEdgeProject(project), nil
}

func normalizeEdgeProject(project EdgeProject) EdgeProject {
	project.ID = strings.TrimSpace(project.ID)
	project.Name = strings.TrimSpace(project.Name)
	project.TenantID = strings.TrimSpace(project.TenantID)
	if project.TenantID == "" {
		project.TenantID = defaultEdgeProjectTenantID
	}
	project.UpstreamURL = strings.TrimSpace(project.UpstreamURL)
	project.HostPolicy = strings.TrimSpace(strings.ToLower(project.HostPolicy))
	if project.HostPolicy == "" {
		project.HostPolicy = edgeRouteHostUpstream
	}
	project.DefaultMode = strings.TrimSpace(strings.ToLower(project.DefaultMode))
	if project.DefaultMode == "" {
		project.DefaultMode = edgeRouteModeObserve
	}
	project.Description = strings.TrimSpace(project.Description)
	hosts := make([]string, 0, len(project.Hosts))
	seen := make(map[string]bool)
	for _, host := range project.Hosts {
		host = normalizeEdgeHost(host)
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		hosts = append(hosts, host)
	}
	project.Hosts = hosts
	return project
}

func NewEdgeRouteManager(db *sql.DB, exportPath string, projects *EdgeProjectManager) (*EdgeRouteManager, error) {
	if db == nil {
		return nil, errors.New("edge route database is nil")
	}
	if projects == nil {
		return nil, errors.New("edge project manager is nil")
	}
	exportPath = strings.TrimSpace(exportPath)
	if exportPath == "" {
		exportPath = defaultEdgeRoutesExportPath
	}
	m := &EdgeRouteManager{db: db, exportPath: exportPath, projects: projects}
	if err := m.initSchema(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *EdgeRouteManager) initSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS edge_routes (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			project_name TEXT NOT NULL DEFAULT '',
			path_prefix TEXT NOT NULL,
			match_type TEXT NOT NULL DEFAULT 'prefix',
			mode TEXT NOT NULL DEFAULT 'observe',
			enabled INTEGER NOT NULL DEFAULT 0,
			priority INTEGER NOT NULL DEFAULT 100,
			description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`ALTER TABLE edge_routes ADD COLUMN match_type TEXT NOT NULL DEFAULT 'prefix'`,
		`CREATE TABLE IF NOT EXISTS edge_route_sync_state (
			id INTEGER PRIMARY KEY,
			version INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tap_exchange_events (
			id INTEGER GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
			created_at TEXT NOT NULL,
			route_id TEXT NOT NULL,
			project_id TEXT NOT NULL,
			project_name TEXT NOT NULL DEFAULT '',
			mode TEXT NOT NULL,
			trace_id TEXT NOT NULL DEFAULT '',
			method TEXT NOT NULL DEFAULT '',
			host TEXT NOT NULL DEFAULT '',
			uri TEXT NOT NULL DEFAULT '',
			upstream_url TEXT NOT NULL DEFAULT '',
			upstream_status INTEGER NOT NULL DEFAULT 0,
			response_status INTEGER NOT NULL DEFAULT 0,
			duration_ms REAL NOT NULL DEFAULT 0,
			action TEXT NOT NULL DEFAULT 'log',
			reason TEXT NOT NULL DEFAULT '',
			request_preview TEXT NOT NULL DEFAULT '',
			request_body_hash TEXT NOT NULL DEFAULT '',
			response_preview TEXT NOT NULL DEFAULT '',
			response_body_hash TEXT NOT NULL DEFAULT ''
		)`,
		`ALTER TABLE tap_exchange_events ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'default'`,
	}
	for _, stmt := range stmts {
		if _, err := m.db.Exec(stmt); err != nil {
			lowerErr := strings.ToLower(err.Error())
			if strings.Contains(lowerErr, "duplicate column") || strings.Contains(lowerErr, "already exists") {
				continue
			}
			return err
		}
	}
	_, err := m.db.Exec(`INSERT INTO edge_route_sync_state (id, version, updated_at)
		VALUES (1, 0, ?)
		ON CONFLICT (id) DO NOTHING`, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (m *EdgeRouteManager) ListRoutes() ([]EdgeRoute, error) {
	rows, err := m.db.Query(`SELECT er.id, er.project_id, COALESCE(ep.name, er.project_name), er.path_prefix,
		COALESCE(er.match_type, 'prefix'), er.mode, er.enabled, er.priority, er.description, er.created_at, er.updated_at
		FROM edge_routes er LEFT JOIN edge_projects ep ON ep.id=er.project_id
		ORDER BY er.project_id ASC, er.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var routes []EdgeRoute
	for rows.Next() {
		route, err := scanEdgeRoute(rows)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

func (m *EdgeRouteManager) CreateRoute(route EdgeRoute) (EdgeRoute, error) {
	route = normalizeEdgeRoute(route)
	project, err := m.validateRouteProject(route.ProjectID)
	if err != nil {
		return EdgeRoute{}, err
	}
	route.ProjectName = project.Name
	if err := m.ValidateRoute(route, ""); err != nil {
		return EdgeRoute{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = m.db.Exec(`INSERT INTO edge_routes
		(id, project_id, project_name, path_prefix, match_type, mode, enabled, priority, description, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		route.ID, route.ProjectID, route.ProjectName, route.PathPrefix, route.MatchType, route.Mode,
		edgeBoolToInt(route.Enabled), route.Priority, route.Description, now, now)
	if err != nil {
		return EdgeRoute{}, err
	}
	route.CreatedAt = now
	route.UpdatedAt = now
	return route, nil
}

func (m *EdgeRouteManager) UpdateRoute(id string, route EdgeRoute) (EdgeRoute, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return EdgeRoute{}, errors.New("route id required")
	}
	route.ID = id
	route = normalizeEdgeRoute(route)
	project, err := m.validateRouteProject(route.ProjectID)
	if err != nil {
		return EdgeRoute{}, err
	}
	route.ProjectName = project.Name
	if err := m.ValidateRoute(route, id); err != nil {
		return EdgeRoute{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := m.db.Exec(`UPDATE edge_routes SET
		project_id=?, project_name=?, path_prefix=?, match_type=?, mode=?, enabled=?, priority=?, description=?, updated_at=?
		WHERE id=?`,
		route.ProjectID, route.ProjectName, route.PathPrefix, route.MatchType, route.Mode,
		edgeBoolToInt(route.Enabled), route.Priority, route.Description, now, route.ID)
	if err != nil {
		return EdgeRoute{}, err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return EdgeRoute{}, sql.ErrNoRows
	}
	route.UpdatedAt = now
	return route, nil
}

func (m *EdgeRouteManager) DeleteRoute(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("route id required")
	}
	res, err := m.db.Exec(`DELETE FROM edge_routes WHERE id=?`, id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (m *EdgeRouteManager) ValidateRoute(route EdgeRoute, excludeID string) error {
	route = normalizeEdgeRoute(route)
	if route.ID == "" {
		return errors.New("id required")
	}
	if route.ProjectID == "" {
		return errors.New("project_id required")
	}
	project, err := m.validateRouteProject(route.ProjectID)
	if err != nil {
		return err
	}
	route.ProjectName = project.Name
	if route.PathPrefix == "" {
		return errors.New("path_prefix required")
	}
	if !strings.HasPrefix(route.PathPrefix, "/") {
		return errors.New("path_prefix must start with /")
	}
	if route.PathPrefix == "/" {
		return errors.New("path_prefix / is reserved for project default ingress")
	}
	if route.MatchType != edgeRouteMatchPrefix && route.MatchType != edgeRouteMatchExact {
		return errors.New("match_type must be prefix or exact")
	}
	if route.Mode != edgeRouteModeObserve && route.Mode != edgeRouteModeEnforce {
		return errors.New("mode must be observe or enforce")
	}
	if route.Enabled {
		if err := m.ensureNoEnabledConflict(route, excludeID); err != nil {
			return err
		}
	}
	return nil
}

func (m *EdgeRouteManager) ExportRoutes() (EdgeRouteExport, error) {
	routes, err := m.projectIngressRoutes()
	if err != nil {
		return EdgeRouteExport{}, err
	}
	for _, route := range routes {
		if err := validateEdgeIngressRoute(route); err != nil {
			return EdgeRouteExport{}, fmt.Errorf("project %s ingress invalid: %w", route.ProjectID, err)
		}
	}
	sortEdgeIngressRoutesForMatch(routes)
	version, err := m.currentVersion()
	if err != nil {
		return EdgeRouteExport{}, err
	}
	export := EdgeRouteExport{
		Version:     version,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Routes:      routes,
	}
	export.Checksum = edgeRouteChecksum(export)
	return export, nil
}

func (m *EdgeRouteManager) SyncRoutes() (EdgeRouteSyncResult, error) {
	routes, err := m.projectIngressRoutes()
	if err != nil {
		return EdgeRouteSyncResult{}, err
	}
	for _, route := range routes {
		if err := validateEdgeIngressRoute(route); err != nil {
			return EdgeRouteSyncResult{}, fmt.Errorf("project %s ingress invalid: %w", route.ProjectID, err)
		}
	}
	sortEdgeIngressRoutesForMatch(routes)

	version, err := m.nextVersion()
	if err != nil {
		return EdgeRouteSyncResult{}, err
	}
	export := EdgeRouteExport{
		Version:     version,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Routes:      routes,
	}
	export.Checksum = edgeRouteChecksum(export)
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return EdgeRouteSyncResult{}, err
	}
	data = append(data, '\n')
	if err := writeFileAtomic(m.exportPath, data, 0644); err != nil {
		return EdgeRouteSyncResult{}, err
	}
	if err := m.setVersion(version); err != nil {
		return EdgeRouteSyncResult{}, err
	}
	return EdgeRouteSyncResult{
		Path:        m.exportPath,
		Version:     version,
		GeneratedAt: export.GeneratedAt,
		Checksum:    export.Checksum,
		Total:       len(routes),
	}, nil
}

func (m *EdgeRouteManager) RecordTapExchange(event TapExchangeEvent) error {
	event = normalizeTapExchangeEvent(event)
	project, err := m.validateRouteProject(event.ProjectID)
	if err != nil {
		return err
	}
	event.ProjectName = project.Name
	event.TenantID = project.TenantID
	_, err = m.db.Exec(`INSERT INTO tap_exchange_events
		(created_at, route_id, project_id, project_name, tenant_id, mode, trace_id, method, host, uri,
		upstream_url, upstream_status, response_status, duration_ms, action, reason,
		request_preview, request_body_hash, response_preview, response_body_hash)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		event.CreatedAt, event.RouteID, event.ProjectID, event.ProjectName, event.TenantID, event.Mode, event.TraceID,
		event.Method, event.Host, event.URI, event.UpstreamURL, event.UpstreamStatus, event.ResponseStatus,
		event.DurationMs, event.Action, event.Reason, event.RequestPreview, event.RequestBodyHash,
		event.ResponsePreview, event.ResponseBodyHash)
	return err
}

func (m *EdgeRouteManager) ListTapExchangeEvents(q TapExchangeEventQuery) ([]TapExchangeEvent, int, error) {
	where := []string{"1=1"}
	args := []interface{}{}
	addText := func(field, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		where = append(where, field+"=?")
		args = append(args, value)
	}
	addText("project_id", q.ProjectID)
	addText("route_id", q.RouteID)
	addText("mode", strings.ToLower(q.Mode))
	if q.Status > 0 {
		where = append(where, "response_status=?")
		args = append(args, q.Status)
	}
	if strings.TrimSpace(q.From) != "" {
		where = append(where, "created_at>=?")
		args = append(args, strings.TrimSpace(q.From))
	}
	if strings.TrimSpace(q.To) != "" {
		where = append(where, "created_at<=?")
		args = append(args, strings.TrimSpace(q.To))
	}
	if strings.TrimSpace(q.Q) != "" {
		like := "%" + strings.TrimSpace(q.Q) + "%"
		where = append(where, "(project_name LIKE ? OR route_id LIKE ? OR host LIKE ? OR uri LIKE ? OR request_preview LIKE ? OR response_preview LIKE ? OR reason LIKE ?)")
		args = append(args, like, like, like, like, like, like, like)
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := m.db.QueryRow("SELECT COUNT(*) FROM tap_exchange_events WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	queryArgs := append([]interface{}{}, args...)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := m.db.Query(`SELECT id, created_at, route_id, project_id, project_name, tenant_id, mode, trace_id,
		method, host, uri, upstream_url, upstream_status, response_status, duration_ms, action, reason,
		request_preview, request_body_hash, response_preview, response_body_hash
		FROM tap_exchange_events WHERE `+whereSQL+`
		ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	events := []TapExchangeEvent{}
	for rows.Next() {
		var event TapExchangeEvent
		if err := rows.Scan(&event.ID, &event.CreatedAt, &event.RouteID, &event.ProjectID, &event.ProjectName, &event.TenantID,
			&event.Mode, &event.TraceID, &event.Method, &event.Host, &event.URI, &event.UpstreamURL,
			&event.UpstreamStatus, &event.ResponseStatus, &event.DurationMs, &event.Action, &event.Reason,
			&event.RequestPreview, &event.RequestBodyHash, &event.ResponsePreview, &event.ResponseBodyHash); err != nil {
			return nil, 0, err
		}
		events = append(events, event)
	}
	return events, total, rows.Err()
}

func (m *EdgeRouteManager) enabledRoutes() ([]EdgeRoute, error) {
	rows, err := m.db.Query(`SELECT er.id, er.project_id, ep.name, er.path_prefix,
		COALESCE(er.match_type, 'prefix'), er.mode, er.enabled, er.priority, er.description, er.created_at, er.updated_at
		FROM edge_routes er JOIN edge_projects ep ON ep.id=er.project_id
		WHERE er.enabled=1 AND ep.enabled=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var routes []EdgeRoute
	for rows.Next() {
		route, err := scanEdgeRoute(rows)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

func (m *EdgeRouteManager) projectIngressRoutes() ([]EdgeIngressRoute, error) {
	projects, err := m.projects.ListProjects()
	if err != nil {
		return nil, err
	}
	routes := []EdgeIngressRoute{}
	for _, project := range projects {
		if !project.Enabled {
			continue
		}
		routes = append(routes, EdgeIngressRoute{
			ID:          project.ID,
			ProjectID:   project.ID,
			ProjectName: project.Name,
			Host:        project.Hosts[0],
			PathPrefix:  "/",
			Mode:        project.DefaultMode,
			UpstreamURL: project.UpstreamURL,
			HostPolicy:  project.HostPolicy,
			Enabled:     project.Enabled,
			Priority:    0,
			Description: "项目默认入口",
		})
	}
	return routes, nil
}

func (m *EdgeRouteManager) validateRouteProject(projectID string) (EdgeProject, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return EdgeProject{}, errors.New("project_id required")
	}
	project, err := m.projects.ResolveEnabledProject(projectID)
	if err == sql.ErrNoRows {
		return EdgeProject{}, fmt.Errorf("project_id %s not found", projectID)
	}
	return project, err
}

func (m *EdgeRouteManager) ResolveMode(projectID, rawURI, fallbackMode string) (string, EdgeRoute, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return normalizeEdgeMode(fallbackMode), EdgeRoute{}, nil
	}
	routes, err := m.enabledRoutesForProject(projectID)
	if err != nil {
		return "", EdgeRoute{}, err
	}
	sortEdgeRoutesForMatch(routes)
	path := normalizeEdgePath(rawURI)
	for _, route := range routes {
		if edgeRouteMatchesPath(route, path) {
			return route.Mode, route, nil
		}
	}
	return normalizeEdgeMode(fallbackMode), EdgeRoute{}, nil
}

func (m *EdgeRouteManager) enabledRoutesForProject(projectID string) ([]EdgeRoute, error) {
	rows, err := m.db.Query(`SELECT er.id, er.project_id, ep.name, er.path_prefix,
		COALESCE(er.match_type, 'prefix'), er.mode, er.enabled, er.priority, er.description, er.created_at, er.updated_at
		FROM edge_routes er JOIN edge_projects ep ON ep.id=er.project_id
		WHERE er.enabled=1 AND ep.enabled=1 AND er.project_id=?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var routes []EdgeRoute
	for rows.Next() {
		route, err := scanEdgeRoute(rows)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

func edgeRouteMatchesPath(route EdgeRoute, path string) bool {
	path = normalizeEdgePath(path)
	prefix := normalizeEdgePath(route.PathPrefix)
	switch route.MatchType {
	case edgeRouteMatchExact:
		return path == prefix
	default:
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
}

func normalizeEdgeMode(mode string) string {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == edgeRouteModeEnforce {
		return edgeRouteModeEnforce
	}
	return edgeRouteModeObserve
}

func (m *EdgeRouteManager) ensureNoEnabledConflict(route EdgeRoute, excludeID string) error {
	routes, err := m.enabledRoutes()
	if err != nil {
		return err
	}
	excludeID = strings.TrimSpace(excludeID)
	for _, existing := range routes {
		if existing.ID == excludeID {
			continue
		}
		if existing.ProjectID == route.ProjectID && existing.PathPrefix == route.PathPrefix &&
			existing.MatchType == route.MatchType && existing.Priority == route.Priority {
			return fmt.Errorf("duplicate enabled uri rule conflict: project/path/match_type/priority %s|%s|%s|%d",
				route.ProjectID, route.PathPrefix, route.MatchType, route.Priority)
		}
	}
	return nil
}

func (m *EdgeRouteManager) currentVersion() (int64, error) {
	var version int64
	err := m.db.QueryRow(`SELECT version FROM edge_route_sync_state WHERE id=1`).Scan(&version)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return version, err
}

func (m *EdgeRouteManager) nextVersion() (int64, error) {
	version, err := m.currentVersion()
	if err != nil {
		return 0, err
	}
	return version + 1, nil
}

func (m *EdgeRouteManager) setVersion(version int64) error {
	_, err := m.db.Exec(`INSERT INTO edge_route_sync_state (id, version, updated_at)
		VALUES (1, ?, ?)
		ON CONFLICT (id) DO UPDATE SET version=EXCLUDED.version, updated_at=EXCLUDED.updated_at`,
		version, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

type edgeRouteScanner interface {
	Scan(dest ...interface{}) error
}

func scanEdgeRoute(row edgeRouteScanner) (EdgeRoute, error) {
	var route EdgeRoute
	var enabled int
	if err := row.Scan(&route.ID, &route.ProjectID, &route.ProjectName, &route.PathPrefix, &route.MatchType,
		&route.Mode, &enabled, &route.Priority, &route.Description,
		&route.CreatedAt, &route.UpdatedAt); err != nil {
		return EdgeRoute{}, err
	}
	route.Enabled = enabled != 0
	return normalizeEdgeRoute(route), nil
}

func normalizeEdgeRoute(route EdgeRoute) EdgeRoute {
	route.ID = strings.TrimSpace(route.ID)
	route.ProjectID = strings.TrimSpace(route.ProjectID)
	route.ProjectName = strings.TrimSpace(route.ProjectName)
	route.PathPrefix = normalizeEdgePath(route.PathPrefix)
	route.MatchType = strings.TrimSpace(strings.ToLower(route.MatchType))
	if route.MatchType == "" {
		route.MatchType = edgeRouteMatchPrefix
	}
	route.Mode = strings.TrimSpace(strings.ToLower(route.Mode))
	if route.Mode == "" {
		route.Mode = edgeRouteModeObserve
	}
	if route.Priority == 0 {
		route.Priority = 100
	}
	route.Description = strings.TrimSpace(route.Description)
	return route
}

func normalizeEdgePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if parsed, err := url.Parse(path); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		path = parsed.Path
	}
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		path = path[:idx]
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	return path
}

func normalizeEdgeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimSuffix(host, ".")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host
}

func validateEdgeHost(host string) error {
	if host == "" {
		return errors.New("host must not be empty")
	}
	if strings.Contains(host, "://") || strings.Contains(host, "/") || strings.ContainsAny(host, " \t\r\n") {
		return fmt.Errorf("invalid host: %s", host)
	}
	return nil
}

func validateEdgeUpstreamURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid upstream_url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("upstream_url scheme must be http or https")
	}
	if u.Host == "" {
		return errors.New("upstream_url host required")
	}
	return nil
}

func sortEdgeRoutesForMatch(routes []EdgeRoute) {
	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].Priority != routes[j].Priority {
			return routes[i].Priority > routes[j].Priority
		}
		if routes[i].MatchType != routes[j].MatchType {
			return routes[i].MatchType == edgeRouteMatchExact
		}
		if len(routes[i].PathPrefix) != len(routes[j].PathPrefix) {
			return len(routes[i].PathPrefix) > len(routes[j].PathPrefix)
		}
		return routes[i].ID < routes[j].ID
	})
}

func sortEdgeIngressRoutesForMatch(routes []EdgeIngressRoute) {
	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].Host != routes[j].Host {
			return routes[i].Host < routes[j].Host
		}
		return routes[i].ProjectID < routes[j].ProjectID
	})
}

func validateEdgeIngressRoute(route EdgeIngressRoute) error {
	if route.ProjectID == "" {
		return errors.New("project_id required")
	}
	if err := validateEdgeHost(route.Host); err != nil {
		return err
	}
	if err := validateEdgeUpstreamURL(route.UpstreamURL); err != nil {
		return err
	}
	if route.HostPolicy != edgeRouteHostPreserve && route.HostPolicy != edgeRouteHostUpstream {
		return errors.New("host_policy must be preserve or upstream_host")
	}
	if route.Mode != edgeRouteModeObserve && route.Mode != edgeRouteModeEnforce {
		return errors.New("mode must be observe or enforce")
	}
	return nil
}

func edgeRouteChecksum(export EdgeRouteExport) string {
	sum := sha256.Sum256([]byte(edgeRouteChecksumPayload(export)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func edgeRouteChecksumPayload(export EdgeRouteExport) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("version=%d\n", export.Version))
	b.WriteString("generated_at=")
	b.WriteString(export.GeneratedAt)
	b.WriteString("\n")
	for _, route := range export.Routes {
		b.WriteString(route.ID)
		b.WriteByte('\t')
		b.WriteString(route.ProjectID)
		b.WriteByte('\t')
		b.WriteString(route.ProjectName)
		b.WriteByte('\t')
		b.WriteString(route.Host)
		b.WriteByte('\t')
		b.WriteString(route.PathPrefix)
		b.WriteByte('\t')
		b.WriteString(route.Mode)
		b.WriteByte('\t')
		b.WriteString(route.UpstreamURL)
		b.WriteByte('\t')
		b.WriteString(route.HostPolicy)
		b.WriteByte('\t')
		b.WriteString(fmt.Sprintf("%t", route.Enabled))
		b.WriteByte('\t')
		b.WriteString(fmt.Sprintf("%d", route.Priority))
		b.WriteByte('\t')
		b.WriteString(route.Description)
		b.WriteByte('\n')
	}
	return b.String()
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func edgeBoolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func normalizeTapExchangeEvent(event TapExchangeEvent) TapExchangeEvent {
	event.CreatedAt = strings.TrimSpace(event.CreatedAt)
	if event.CreatedAt == "" {
		event.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	event.RouteID = strings.TrimSpace(event.RouteID)
	event.ProjectID = strings.TrimSpace(event.ProjectID)
	event.ProjectName = strings.TrimSpace(event.ProjectName)
	event.TenantID = strings.TrimSpace(event.TenantID)
	if event.TenantID == "" {
		event.TenantID = defaultEdgeProjectTenantID
	}
	event.Mode = strings.TrimSpace(strings.ToLower(event.Mode))
	if event.Mode == "" {
		event.Mode = edgeRouteModeObserve
	}
	event.TraceID = strings.TrimSpace(event.TraceID)
	event.Method = strings.TrimSpace(strings.ToUpper(event.Method))
	event.Host = strings.TrimSpace(strings.ToLower(event.Host))
	event.URI = strings.TrimSpace(event.URI)
	event.UpstreamURL = strings.TrimSpace(event.UpstreamURL)
	event.Action = ""
	event.Reason = ""
	event.RequestPreview = trimTapPreview(event.RequestPreview)
	event.ResponsePreview = trimTapPreview(event.ResponsePreview)
	event.RequestBodyHash = strings.TrimSpace(event.RequestBodyHash)
	event.ResponseBodyHash = strings.TrimSpace(event.ResponseBodyHash)
	return event
}

func trimTapPreview(s string) string {
	const max = 8192
	rs := []rune(strings.TrimSpace(s))
	if len(rs) > max {
		return string(rs[:max])
	}
	return string(rs)
}
