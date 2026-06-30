package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func newTestEdgeManagers(t *testing.T) (*sql.DB, *EdgeProjectManager, *EdgeRouteManager) {
	t.Helper()
	db := openTestDB(t)
	projects, err := NewEdgeProjectManager(db)
	if err != nil {
		t.Fatalf("NewEdgeProjectManager failed: %v", err)
	}
	routes, err := NewEdgeRouteManager(db, filepath.Join(t.TempDir(), "edge-routes.json"), projects)
	if err != nil {
		t.Fatalf("NewEdgeRouteManager failed: %v", err)
	}
	return db, projects, routes
}

func mustCreateEdgeProject(t *testing.T, mgr *EdgeProjectManager, id, name string) EdgeProject {
	t.Helper()
	project, err := mgr.CreateProject(EdgeProject{
		ID: id, Name: name, Enabled: true,
		Hosts: []string{id + ".example.com"}, UpstreamURL: "https://" + id + ".origin", DefaultMode: edgeRouteModeObserve,
	})
	if err != nil {
		t.Fatalf("CreateProject(%s) failed: %v", id, err)
	}
	return project
}

func TestEdgeProjectCRUDAndDeleteGuard(t *testing.T) {
	_, projects, routes := newTestEdgeManagers(t)

	project, err := projects.CreateProject(EdgeProject{
		ID: "openclaw-lanxin-test", Name: "OpenClaw Lanxin Test",
		Hosts: []string{"openclaw.local"}, UpstreamURL: "http://127.0.0.1:18790",
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	if project.TenantID != defaultEdgeProjectTenantID || project.Enabled {
		t.Fatalf("unexpected project defaults: %+v", project)
	}

	project.Enabled = true
	project.Description = "customer test"
	project, err = projects.UpdateProject(project.ID, project)
	if err != nil {
		t.Fatalf("UpdateProject failed: %v", err)
	}
	if !project.Enabled || project.Description != "customer test" {
		t.Fatalf("project not updated: %+v", project)
	}

	got, err := projects.GetProject(project.ID)
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}
	if got.Name != "OpenClaw Lanxin Test" {
		t.Fatalf("project name = %q", got.Name)
	}

	if _, err := routes.CreateRoute(EdgeRoute{
		ID:          "openclaw-route",
		ProjectID:   project.ID,
		ProjectName: "stale name",
		PathPrefix:  "/api",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("CreateRoute failed: %v", err)
	}
	if err := projects.DeleteProject(project.ID); !errors.Is(err, ErrEdgeProjectInUse) {
		t.Fatalf("DeleteProject referenced error = %v, want ErrEdgeProjectInUse", err)
	}

	if err := routes.DeleteRoute("openclaw-route"); err != nil {
		t.Fatalf("DeleteRoute failed: %v", err)
	}
	if err := projects.DeleteProject(project.ID); err != nil {
		t.Fatalf("DeleteProject failed: %v", err)
	}
}

func TestEdgeProjectValidationRejectsInvalidTenant(t *testing.T) {
	db, projects, _ := newTestEdgeManagers(t)
	NewTenantManager(db)

	if err := projects.ValidateProject(EdgeProject{ID: "", Name: "x", Enabled: true}); err == nil {
		t.Fatalf("expected empty id validation error")
	}
	if err := projects.ValidateProject(EdgeProject{ID: "p1", Name: "", Hosts: []string{"p1.example.com"}, UpstreamURL: "https://origin.example", Enabled: true}); err == nil {
		t.Fatalf("expected empty name validation error")
	}
	if err := projects.ValidateProject(EdgeProject{ID: "p1", Name: "P1", TenantID: "missing", Hosts: []string{"p1.example.com"}, UpstreamURL: "https://origin.example", Enabled: true}); err == nil {
		t.Fatalf("expected missing tenant validation error")
	}
}

func TestEdgeRouteDefaultsValidateAndProjectSync(t *testing.T) {
	_, projects, mgr := newTestEdgeManagers(t)
	project := mustCreateEdgeProject(t, projects, "crm-copilot", "CRM Assistant")

	created, err := mgr.CreateRoute(EdgeRoute{
		ID:          "crm-chat",
		ProjectID:   "crm-copilot",
		ProjectName: "stale client value",
		PathPrefix:  "/api/chat",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("CreateRoute failed: %v", err)
	}
	if created.Mode != edgeRouteModeObserve {
		t.Fatalf("mode default = %q, want observe", created.Mode)
	}
	if created.MatchType != edgeRouteMatchPrefix {
		t.Fatalf("match_type default = %q, want prefix", created.MatchType)
	}
	if created.Priority != 100 {
		t.Fatalf("priority default = %d, want 100", created.Priority)
	}
	if created.ProjectName != "CRM Assistant" {
		t.Fatalf("project_name = %q, want DB project name", created.ProjectName)
	}

	result, err := mgr.SyncRoutes()
	if err != nil {
		t.Fatalf("SyncRoutes failed: %v", err)
	}
	if result.Version != 1 || result.Total != 1 || result.Checksum == "" {
		t.Fatalf("unexpected sync result: %+v", result)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("read export file failed: %v", err)
	}
	var export EdgeRouteExport
	if err := json.Unmarshal(data, &export); err != nil {
		t.Fatalf("export JSON invalid: %v", err)
	}
	if export.Checksum != edgeRouteChecksum(export) {
		t.Fatalf("checksum mismatch: %s", export.Checksum)
	}
	if export.Routes[0].ProjectName != "CRM Assistant" {
		t.Fatalf("export project_name = %q", export.Routes[0].ProjectName)
	}
	if export.Routes[0].ID != project.ID || export.Routes[0].ProjectID != project.ID {
		t.Fatalf("export id should use project id: %+v", export.Routes[0])
	}
	if export.Routes[0].Host != project.Hosts[0] || export.Routes[0].PathPrefix != "/" || export.Routes[0].UpstreamURL != project.UpstreamURL {
		t.Fatalf("export should use project ingress: %+v", export.Routes[0])
	}
}

func TestEdgeRouteValidationRejectsInvalidFields(t *testing.T) {
	_, projects, mgr := newTestEdgeManagers(t)
	mustCreateEdgeProject(t, projects, "p1", "Project One")

	cases := []EdgeRoute{
		{ID: "r1", PathPrefix: "/api", Enabled: true},
		{ID: "r2", ProjectID: "p1", PathPrefix: "/", Enabled: true},
		{ID: "r3", ProjectID: "p1", PathPrefix: "/api", MatchType: "regex", Enabled: true},
		{ID: "r4", ProjectID: "p1", PathPrefix: "/api", Mode: "block", Enabled: true},
	}
	for _, tc := range cases {
		if err := mgr.ValidateRoute(normalizeEdgeRoute(tc), ""); err == nil {
			t.Fatalf("ValidateRoute(%s) expected error", tc.ID)
		}
	}
}

func TestEdgeRouteDuplicateConflictAndExportOrder(t *testing.T) {
	_, projects, mgr := newTestEdgeManagers(t)
	mustCreateEdgeProject(t, projects, "p", "Project")
	routes := []EdgeRoute{
		{ID: "low", ProjectID: "p", PathPrefix: "/api", Enabled: true, Priority: 50},
		{ID: "high", ProjectID: "p", PathPrefix: "/api", MatchType: edgeRouteMatchExact, Enabled: true, Priority: 200},
		{ID: "deep", ProjectID: "p", PathPrefix: "/api/chat", Enabled: true, Priority: 200},
	}
	for _, route := range routes {
		if _, err := mgr.CreateRoute(route); err != nil {
			t.Fatalf("CreateRoute(%s) failed: %v", route.ID, err)
		}
	}
	if _, err := mgr.CreateRoute(EdgeRoute{ID: "dup", ProjectID: "p", PathPrefix: "/api", Enabled: true, Priority: 50}); err == nil {
		t.Fatalf("expected duplicate conflict")
	}

	ruleMode, matched, err := mgr.ResolveMode("p", "/api/chat/send?x=1", edgeRouteModeObserve)
	if err != nil {
		t.Fatalf("ResolveMode failed: %v", err)
	}
	if ruleMode != edgeRouteModeObserve || matched.ID != "deep" {
		t.Fatalf("matched = %s mode=%s, want deep observe", matched.ID, ruleMode)
	}
	sortEdgeRoutesForMatch(routes)
	got := []string{routes[0].ID, routes[1].ID, routes[2].ID}
	want := []string{"deep", "high", "low"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("export order = %v, want %v", got, want)
		}
	}
}

func TestEdgeRouteRequiresEnabledProjectAndSkipsDisabledProjectOnExport(t *testing.T) {
	_, projects, mgr := newTestEdgeManagers(t)
	if _, err := mgr.CreateRoute(EdgeRoute{ID: "missing", ProjectID: "missing", PathPrefix: "/api", Enabled: true}); err == nil {
		t.Fatalf("expected missing project error")
	}
	project := mustCreateEdgeProject(t, projects, "p", "Project")
	if _, err := mgr.CreateRoute(EdgeRoute{ID: "r1", ProjectID: project.ID, PathPrefix: "/api", Enabled: true}); err != nil {
		t.Fatalf("CreateRoute failed: %v", err)
	}
	project.Enabled = false
	if _, err := projects.UpdateProject(project.ID, project); err != nil {
		t.Fatalf("disable project failed: %v", err)
	}
	export, err := mgr.ExportRoutes()
	if err != nil {
		t.Fatalf("ExportRoutes failed: %v", err)
	}
	if len(export.Routes) != 0 {
		t.Fatalf("disabled project ingress exported: %+v", export.Routes)
	}
}

func TestExchangeObserveAndEnforce(t *testing.T) {
	db := openTestDB(t)
	logger, err := NewAuditLogger(db)
	if err != nil {
		t.Fatalf("NewAuditLogger failed: %v", err)
	}
	t.Cleanup(logger.Close)

	cfg := &Config{EdgeRoutesExportPath: filepath.Join(t.TempDir(), "edge-routes.json")}
	api := NewManagementAPI(cfg, "", nil, nil, logger, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	mustCreateEdgeProject(t, api.edgeProjects, "p1", "Project One")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/base/api/chat" {
			t.Fatalf("upstream path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	for _, mode := range []string{"observe", "enforce"} {
		req := httptest.NewRequest(http.MethodPost, "/__lobster/exchange", strings.NewReader(`{"prompt":"hello"}`))
		req.Header.Set("X-Lobster-Mode", mode)
		req.Header.Set("X-Lobster-Route-ID", "r1")
		req.Header.Set("X-Lobster-Project-ID", "p1")
		req.Header.Set("X-Lobster-Upstream-URL", upstream.URL+"/base")
		req.Header.Set("X-Lobster-Original-URI", "/api/chat?x=1")
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code != 201 {
			t.Fatalf("%s exchange status = %d body=%s", mode, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"ok":true`) {
			t.Fatalf("unexpected %s body: %s", mode, rec.Body.String())
		}
	}

	var tenantID, projectName string
	if err := db.QueryRow(`SELECT tenant_id, project_name FROM tap_exchange_events WHERE project_id=? ORDER BY id DESC LIMIT 1`, "p1").Scan(&tenantID, &projectName); err != nil {
		t.Fatalf("query tap_exchange_events failed: %v", err)
	}
	if tenantID != defaultEdgeProjectTenantID || projectName != "Project One" {
		t.Fatalf("tap event project fields = tenant %q project %q", tenantID, projectName)
	}

	var passAudits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE app_id=? AND action='pass'`, "p1").Scan(&passAudits); err != nil {
		t.Fatalf("query pass audit logs failed: %v", err)
	}
	if passAudits != 2 {
		t.Fatalf("pass audit logs = %d, want 2", passAudits)
	}
}

func TestExchangeWebSocketObserveProxy(t *testing.T) {
	db := openTestDB(t)
	logger, err := NewAuditLogger(db)
	if err != nil {
		t.Fatalf("NewAuditLogger failed: %v", err)
	}
	t.Cleanup(logger.Close)

	cfg := &Config{EdgeRoutesExportPath: filepath.Join(t.TempDir(), "edge-routes.json")}
	api := NewManagementAPI(cfg, "", nil, nil, logger, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	mustCreateEdgeProject(t, api.edgeProjects, "p1", "Project One")

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		mt, data, err := c.ReadMessage()
		if err != nil {
			return
		}
		_ = c.WriteMessage(mt, append([]byte("echo:"), data...))
	}))
	defer upstream.Close()

	exchangeServer := httptest.NewServer(api)
	defer exchangeServer.Close()

	wsURL := "ws" + strings.TrimPrefix(exchangeServer.URL, "http") + "/__lobster/exchange"
	header := http.Header{}
	header.Set("X-Lobster-Mode", "observe")
	header.Set("X-Lobster-Route-ID", "r1")
	header.Set("X-Lobster-Project-ID", "p1")
	header.Set("X-Lobster-Upstream-URL", upstream.URL)
	header.Set("X-Lobster-Original-URI", "/ws")

	c, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial exchange ws failed: %v", err)
	}
	defer c.Close()
	if err := c.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatalf("write ws failed: %v", err)
	}
	_, data, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read ws failed: %v", err)
	}
	if string(data) != "echo:hello" {
		t.Fatalf("ws response = %q", string(data))
	}
}

func TestExchangeWebSocketEnforceBlocksFrameKeepsConnection(t *testing.T) {
	db := openTestDB(t)
	logger, err := NewAuditLogger(db)
	if err != nil {
		t.Fatalf("NewAuditLogger failed: %v", err)
	}
	t.Cleanup(logger.Close)

	blockRuleEnabled := true
	engine := NewRuleEngineFromConfig([]InboundRuleConfig{
		{Name: "block-secret", Patterns: []string{"secret"}, Action: "block", Enabled: &blockRuleEnabled},
	}, "test")
	cfg := &Config{EdgeRoutesExportPath: filepath.Join(t.TempDir(), "edge-routes.json")}
	api := NewManagementAPI(cfg, "", nil, nil, logger, engine, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	mustCreateEdgeProject(t, api.edgeProjects, "p1", "Project One")

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	upstreamMessages := make(chan string, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for i := 0; i < 1; i++ {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			upstreamMessages <- string(data)
			_ = c.WriteMessage(websocket.TextMessage, append([]byte("echo:"), data...))
		}
	}))
	defer upstream.Close()

	exchangeServer := httptest.NewServer(api)
	defer exchangeServer.Close()

	wsURL := "ws" + strings.TrimPrefix(exchangeServer.URL, "http") + "/__lobster/exchange"
	header := http.Header{}
	header.Set("X-Lobster-Mode", "enforce")
	header.Set("X-Lobster-Route-ID", "r1")
	header.Set("X-Lobster-Project-ID", "p1")
	header.Set("X-Lobster-Upstream-URL", upstream.URL)
	header.Set("X-Lobster-Original-URI", "/ws")

	c, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial exchange ws failed: %v", err)
	}
	defer c.Close()
	if err := c.WriteMessage(websocket.TextMessage, []byte("secret")); err != nil {
		t.Fatalf("write blocked ws frame failed: %v", err)
	}
	_, data, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("read block notice failed: %v", err)
	}
	if !strings.Contains(string(data), "lobster_guard_block") {
		t.Fatalf("expected block notice, got %q", string(data))
	}

	if err := c.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatalf("write allowed ws frame failed: %v", err)
	}
	_, data, err = c.ReadMessage()
	if err != nil {
		t.Fatalf("read allowed ws response failed, connection likely closed: %v", err)
	}
	if string(data) != "echo:hello" {
		t.Fatalf("allowed ws response = %q", string(data))
	}
	select {
	case got := <-upstreamMessages:
		if got != "hello" {
			t.Fatalf("upstream got %q, want only allowed frame", got)
		}
	default:
		t.Fatalf("upstream did not receive allowed frame")
	}
}
