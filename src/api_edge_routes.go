package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

func (api *ManagementAPI) handleListEdgeProjects(w http.ResponseWriter, r *http.Request) {
	if api.edgeProjects == nil {
		jsonResponse(w, 503, map[string]string{"error": "edge projects not initialized"})
		return
	}
	projects, err := api.edgeProjects.ListProjects()
	if err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, map[string]interface{}{"projects": projects, "total": len(projects)})
}

func (api *ManagementAPI) handleCreateEdgeProject(w http.ResponseWriter, r *http.Request) {
	if api.edgeProjects == nil {
		jsonResponse(w, 503, map[string]string{"error": "edge projects not initialized"})
		return
	}
	var project EdgeProject
	if err := json.NewDecoder(r.Body).Decode(&project); err != nil {
		jsonResponse(w, 400, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	created, err := api.edgeProjects.CreateProject(project)
	if err != nil {
		jsonResponse(w, 400, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, map[string]interface{}{"status": "created", "project": created})
}

func (api *ManagementAPI) handleGetEdgeProject(w http.ResponseWriter, r *http.Request) {
	if api.edgeProjects == nil {
		jsonResponse(w, 503, map[string]string{"error": "edge projects not initialized"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/edge-projects/")
	id = strings.TrimSpace(id)
	project, err := api.edgeProjects.GetProject(id)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonResponse(w, 404, map[string]string{"error": "edge project not found"})
			return
		}
		jsonResponse(w, 400, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, project)
}

func (api *ManagementAPI) handleUpdateEdgeProject(w http.ResponseWriter, r *http.Request) {
	if api.edgeProjects == nil {
		jsonResponse(w, 503, map[string]string{"error": "edge projects not initialized"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/edge-projects/")
	id = strings.TrimSpace(id)
	var project EdgeProject
	if err := json.NewDecoder(r.Body).Decode(&project); err != nil {
		jsonResponse(w, 400, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	updated, err := api.edgeProjects.UpdateProject(id, project)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonResponse(w, 404, map[string]string{"error": "edge project not found"})
			return
		}
		jsonResponse(w, 400, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, map[string]interface{}{"status": "updated", "project": updated})
}

func (api *ManagementAPI) handleDeleteEdgeProject(w http.ResponseWriter, r *http.Request) {
	if api.edgeProjects == nil {
		jsonResponse(w, 503, map[string]string{"error": "edge projects not initialized"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/edge-projects/")
	id = strings.TrimSpace(id)
	if err := api.edgeProjects.DeleteProject(id); err != nil {
		switch {
		case err == sql.ErrNoRows:
			jsonResponse(w, 404, map[string]string{"error": "edge project not found"})
		case errors.Is(err, ErrEdgeProjectInUse):
			jsonResponse(w, 409, map[string]string{"error": "edge project is referenced by edge routes"})
		default:
			jsonResponse(w, 400, map[string]string{"error": err.Error()})
		}
		return
	}
	jsonResponse(w, 200, map[string]string{"status": "deleted", "id": id})
}

func (api *ManagementAPI) handleValidateEdgeProject(w http.ResponseWriter, r *http.Request) {
	if api.edgeProjects == nil {
		jsonResponse(w, 503, map[string]string{"error": "edge projects not initialized"})
		return
	}
	var project EdgeProject
	if err := json.NewDecoder(r.Body).Decode(&project); err != nil {
		jsonResponse(w, 400, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if err := api.edgeProjects.ValidateProject(project); err != nil {
		jsonResponse(w, 400, map[string]interface{}{"valid": false, "error": err.Error()})
		return
	}
	jsonResponse(w, 200, map[string]interface{}{"valid": true})
}

func (api *ManagementAPI) handleListEdgeRoutes(w http.ResponseWriter, r *http.Request) {
	if api.edgeRoutes == nil {
		jsonResponse(w, 503, map[string]string{"error": "edge routes not initialized"})
		return
	}
	routes, err := api.edgeRoutes.ListRoutes()
	if err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, map[string]interface{}{"routes": routes, "total": len(routes)})
}

func (api *ManagementAPI) handleCreateEdgeRoute(w http.ResponseWriter, r *http.Request) {
	if api.edgeRoutes == nil {
		jsonResponse(w, 503, map[string]string{"error": "edge routes not initialized"})
		return
	}
	var route EdgeRoute
	if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
		jsonResponse(w, 400, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	created, err := api.edgeRoutes.CreateRoute(route)
	if err != nil {
		jsonResponse(w, 400, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, map[string]interface{}{"status": "created", "route": created})
}

func (api *ManagementAPI) handleUpdateEdgeRoute(w http.ResponseWriter, r *http.Request) {
	if api.edgeRoutes == nil {
		jsonResponse(w, 503, map[string]string{"error": "edge routes not initialized"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/edge-routes/")
	id = strings.TrimSpace(id)
	if id == "" {
		jsonResponse(w, 400, map[string]string{"error": "route id required"})
		return
	}
	var route EdgeRoute
	if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
		jsonResponse(w, 400, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	updated, err := api.edgeRoutes.UpdateRoute(id, route)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonResponse(w, 404, map[string]string{"error": "edge route not found"})
			return
		}
		jsonResponse(w, 400, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, map[string]interface{}{"status": "updated", "route": updated})
}

func (api *ManagementAPI) handleDeleteEdgeRoute(w http.ResponseWriter, r *http.Request) {
	if api.edgeRoutes == nil {
		jsonResponse(w, 503, map[string]string{"error": "edge routes not initialized"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/edge-routes/")
	id = strings.TrimSpace(id)
	if id == "" {
		jsonResponse(w, 400, map[string]string{"error": "route id required"})
		return
	}
	if err := api.edgeRoutes.DeleteRoute(id); err != nil {
		if err == sql.ErrNoRows {
			jsonResponse(w, 404, map[string]string{"error": "edge route not found"})
			return
		}
		jsonResponse(w, 400, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, map[string]string{"status": "deleted", "id": id})
}

func (api *ManagementAPI) handleValidateEdgeRoute(w http.ResponseWriter, r *http.Request) {
	if api.edgeRoutes == nil {
		jsonResponse(w, 503, map[string]string{"error": "edge routes not initialized"})
		return
	}
	var route EdgeRoute
	if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
		jsonResponse(w, 400, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if err := api.edgeRoutes.ValidateRoute(normalizeEdgeRoute(route), route.ID); err != nil {
		jsonResponse(w, 400, map[string]interface{}{"valid": false, "error": err.Error()})
		return
	}
	jsonResponse(w, 200, map[string]interface{}{"valid": true})
}

func (api *ManagementAPI) handleSyncEdgeRoutes(w http.ResponseWriter, r *http.Request) {
	if api.edgeRoutes == nil {
		jsonResponse(w, 503, map[string]string{"error": "edge routes not initialized"})
		return
	}
	result, err := api.edgeRoutes.SyncRoutes()
	if err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, map[string]interface{}{"status": "synced", "result": result})
}

func (api *ManagementAPI) handleExportEdgeRoutes(w http.ResponseWriter, r *http.Request) {
	if api.edgeRoutes == nil {
		jsonResponse(w, 503, map[string]string{"error": "edge routes not initialized"})
		return
	}
	export, err := api.edgeRoutes.ExportRoutes()
	if err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResponse(w, 200, export)
}

func (api *ManagementAPI) handleListTapExchangeEvents(w http.ResponseWriter, r *http.Request) {
	if api.edgeRoutes == nil {
		jsonResponse(w, 503, map[string]string{"error": "edge routes not initialized"})
		return
	}
	values := r.URL.Query()
	parseInt := func(key string) int {
		v, _ := strconv.Atoi(strings.TrimSpace(values.Get(key)))
		return v
	}
	query := TapExchangeEventQuery{
		ProjectID: values.Get("project_id"),
		RouteID:   values.Get("route_id"),
		Mode:      values.Get("mode"),
		Status:    parseInt("status"),
		Q:         values.Get("q"),
		From:      values.Get("from"),
		To:        values.Get("to"),
		Limit:     parseInt("limit"),
		Offset:    parseInt("offset"),
	}
	events, total, err := api.edgeRoutes.ListTapExchangeEvents(query)
	if err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	jsonResponse(w, 200, map[string]interface{}{
		"events": events,
		"total":  total,
		"limit":  limit,
		"offset": query.Offset,
	})
}

func (api *ManagementAPI) handleExchange(w http.ResponseWriter, r *http.Request) {
	if api.edgeRoutes == nil {
		jsonResponse(w, 503, map[string]string{"error": "edge routes not initialized"})
		return
	}
	mode := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Lobster-Mode")))
	if mode == "" {
		mode = edgeRouteModeObserve
	}
	api.handleInlineExchange(w, r, mode)
}

func (api *ManagementAPI) handleInlineExchange(w http.ResponseWriter, r *http.Request, mode string) {
	if IsWebSocketUpgrade(r) {
		api.handleWebSocketExchange(w, r, mode)
		return
	}
	start := time.Now()
	mode = normalizeEdgeMode(mode)
	upstreamURL := strings.TrimSpace(r.Header.Get("X-Lobster-Upstream-URL"))
	if upstreamURL == "" {
		jsonResponse(w, 400, map[string]string{"error": "X-Lobster-Upstream-URL required"})
		return
	}
	parsedUpstream, err := url.Parse(upstreamURL)
	if err != nil || parsedUpstream.Scheme == "" || parsedUpstream.Host == "" {
		jsonResponse(w, 400, map[string]string{"error": "invalid X-Lobster-Upstream-URL"})
		return
	}
	projectID := r.Header.Get("X-Lobster-Project-ID")
	project, err := api.edgeRoutes.validateRouteProject(projectID)
	if err != nil {
		jsonResponse(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if project.DefaultMode != "" {
		mode = project.DefaultMode
	}
	resolvedMode, matchedRule, err := api.edgeRoutes.ResolveMode(projectID, originalRequestURI(r), mode)
	if err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	mode = resolvedMode

	body, err := io.ReadAll(io.LimitReader(r.Body, 20<<20))
	if err != nil {
		jsonResponse(w, 400, map[string]string{"error": "read request body failed"})
		return
	}
	requestPreview := trimTapPreview(string(body))
	event := TapExchangeEvent{
		RouteID:        matchedRule.ID,
		ProjectID:      projectID,
		ProjectName:    r.Header.Get("X-Lobster-Project-Name"),
		Mode:           mode,
		TraceID:        r.Header.Get("X-Request-ID"),
		Method:         r.Method,
		Host:           r.Host,
		URI:            originalRequestURI(r),
		UpstreamURL:    upstreamURL,
		RequestPreview: requestPreview,
	}
	if mode == edgeRouteModeEnforce {
		result := api.detectExchangeRequest(event)
		if result.Action == "block" || result.Action == "confirm" {
			event.DurationMs = float64(time.Since(start).Microseconds()) / 1000
			_ = api.edgeRoutes.RecordTapExchange(event)
			api.logExchangeAudit("inbound", event, result, event.DurationMs)
			jsonResponse(w, 403, map[string]string{"error": "blocked", "reason": strings.Join(result.Reasons, ",")})
			return
		}
		api.logExchangeAudit("inbound", event, result, 0)
	}

	resp, err := api.forwardEnforceUpstream(r, upstreamURL, body)
	if err != nil {
		event.DurationMs = float64(time.Since(start).Microseconds()) / 1000
		_ = api.edgeRoutes.RecordTapExchange(event)
		jsonResponse(w, 502, map[string]string{"error": "upstream_error", "message": err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		event.DurationMs = float64(time.Since(start).Microseconds()) / 1000
		_ = api.edgeRoutes.RecordTapExchange(event)
		jsonResponse(w, 502, map[string]string{"error": "response_read_error"})
		return
	}
	event.ResponseStatus = resp.StatusCode
	event.UpstreamStatus = resp.StatusCode
	event.ResponsePreview = trimTapPreview(string(respBody))
	event.DurationMs = float64(time.Since(start).Microseconds()) / 1000
	if mode == edgeRouteModeEnforce {
		result := api.detectExchangeResponse(event)
		api.logExchangeAudit("outbound", event, result, event.DurationMs)
		if result.Action == "block" {
			_ = api.edgeRoutes.RecordTapExchange(event)
			jsonResponse(w, 403, map[string]string{"error": "blocked", "reason": strings.Join(result.Reasons, ",")})
			return
		}
	}

	if err := api.edgeRoutes.RecordTapExchange(event); err != nil {
		jsonResponse(w, 500, map[string]string{"error": err.Error()})
		return
	}
	copyResponseHeaders(w, resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

func (api *ManagementAPI) handleWebSocketExchange(w http.ResponseWriter, r *http.Request, mode string) {
	mode = normalizeEdgeMode(mode)
	upstreamURL := strings.TrimSpace(r.Header.Get("X-Lobster-Upstream-URL"))
	if upstreamURL == "" {
		http.Error(w, "X-Lobster-Upstream-URL required", http.StatusBadRequest)
		return
	}
	projectID := r.Header.Get("X-Lobster-Project-ID")
	project, err := api.edgeRoutes.validateRouteProject(projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if project.DefaultMode != "" {
		mode = project.DefaultMode
	}
	resolvedMode, matchedRule, err := api.edgeRoutes.ResolveMode(projectID, originalRequestURI(r), mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	mode = resolvedMode
	upstreamWSURL, err := buildExchangeWebSocketURL(upstreamURL, originalRequestURI(r))
	if err != nil {
		http.Error(w, "invalid X-Lobster-Upstream-URL", http.StatusBadRequest)
		return
	}
	upstreamHeader := http.Header{}
	for name, values := range r.Header {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-lobster-") || lower == "host" || lower == "connection" || lower == "upgrade" ||
			lower == "sec-websocket-key" || lower == "sec-websocket-version" || lower == "sec-websocket-extensions" {
			continue
		}
		for _, value := range values {
			upstreamHeader.Add(name, value)
		}
	}
	upConn, _, err := websocket.DefaultDialer.Dial(upstreamWSURL, upstreamHeader)
	if err != nil {
		http.Error(w, "upstream WebSocket connection failed", http.StatusBadGateway)
		return
	}

	clientConn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		_ = upConn.Close()
		return
	}

	eventBase := TapExchangeEvent{
		RouteID:     matchedRule.ID,
		ProjectID:   projectID,
		ProjectName: r.Header.Get("X-Lobster-Project-Name"),
		Mode:        mode,
		TraceID:     r.Header.Get("X-Request-ID"),
		Method:      "WEBSOCKET",
		Host:        r.Host,
		URI:         originalRequestURI(r),
		UpstreamURL: upstreamURL,
	}
	_ = api.edgeRoutes.RecordTapExchange(TapExchangeEvent{
		RouteID:     eventBase.RouteID,
		ProjectID:   eventBase.ProjectID,
		ProjectName: eventBase.ProjectName,
		Mode:        mode,
		TraceID:     eventBase.TraceID,
		Method:      "WEBSOCKET",
		Host:        eventBase.Host,
		URI:         eventBase.URI,
		UpstreamURL: upstreamURL,
	})

	done := make(chan struct{})
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = clientConn.Close()
			_ = upConn.Close()
			close(done)
		})
	}
	go api.proxyWebSocketFrames(clientConn, upConn, "inbound", mode, eventBase, closeBoth)
	go api.proxyWebSocketFrames(upConn, clientConn, "outbound", mode, eventBase, closeBoth)
	<-done
}

func (api *ManagementAPI) proxyWebSocketFrames(src, dst *websocket.Conn, direction, mode string, eventBase TapExchangeEvent, closeBoth func()) {
	defer closeBoth()
	for {
		msgType, data, err := src.ReadMessage()
		if err != nil {
			return
		}
		writeData := data
		if msgType == websocket.TextMessage {
			event := eventBase
			if direction == "inbound" {
				event.RequestPreview = trimTapPreview(string(data))
			} else {
				event.ResponsePreview = trimTapPreview(string(data))
			}
			if mode == edgeRouteModeEnforce {
				var result DetectResult
				if direction == "inbound" {
					result = api.detectExchangeRequest(event)
				} else {
					result = api.detectExchangeResponse(event)
				}
				api.logExchangeAudit(direction, event, result, 0)
				if result.Action == "block" {
					_ = api.edgeRoutes.RecordTapExchange(event)
					_ = writeWebSocketBlockNotice(src, direction, strings.Join(result.Reasons, ","))
					continue
				}
			}
			_ = api.edgeRoutes.RecordTapExchange(event)
		}
		if err := dst.WriteMessage(msgType, writeData); err != nil {
			return
		}
	}
}

func writeWebSocketBlockNotice(conn *websocket.Conn, direction, reason string) error {
	notice := map[string]string{
		"type":      "lobster_guard_block",
		"direction": direction,
		"reason":    reason,
	}
	data, err := json.Marshal(notice)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

func buildExchangeWebSocketURL(upstreamURL, originalURI string) (string, error) {
	target, err := url.Parse(upstreamURL)
	if err != nil {
		return "", err
	}
	switch target.Scheme {
	case "http":
		target.Scheme = "ws"
	case "https":
		target.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", errors.New("unsupported upstream scheme")
	}
	sourceURL, err := url.ParseRequestURI(originalURI)
	if err != nil {
		sourceURL = &url.URL{Path: originalURI}
	}
	target.Path = joinURLPath(target.Path, sourceURL.Path)
	target.RawQuery = sourceURL.RawQuery
	return target.String(), nil
}

func (api *ManagementAPI) forwardEnforceUpstream(r *http.Request, upstreamURL string, body []byte) (*http.Response, error) {
	target, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, err
	}
	sourceURL, err := url.ParseRequestURI(originalRequestURI(r))
	if err != nil {
		sourceURL = r.URL
	}
	target.Path = joinURLPath(target.Path, sourceURL.Path)
	target.RawQuery = sourceURL.RawQuery

	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for name, values := range r.Header {
		if strings.HasPrefix(strings.ToLower(name), "x-lobster-") {
			continue
		}
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	req.Header.Set("X-Forwarded-Host", r.Host)
	req.Header.Set("X-Forwarded-For", getRequestIP(r))
	if target.Host != "" {
		req.Host = target.Host
	}
	client := &http.Client{Timeout: 60 * time.Second}
	return client.Do(req)
}

func joinURLPath(basePath, reqPath string) string {
	basePath = strings.TrimRight(basePath, "/")
	if reqPath == "" {
		reqPath = "/"
	}
	if basePath == "" {
		return reqPath
	}
	return basePath + "/" + strings.TrimLeft(reqPath, "/")
}

func originalRequestURI(r *http.Request) string {
	if uri := strings.TrimSpace(r.Header.Get("X-Lobster-Original-URI")); uri != "" {
		return uri
	}
	return r.URL.RequestURI()
}

func copyResponseHeaders(w http.ResponseWriter, header http.Header) {
	for name, values := range header {
		if strings.EqualFold(name, "Content-Length") || strings.EqualFold(name, "Transfer-Encoding") || strings.EqualFold(name, "Connection") {
			continue
		}
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
}

func (api *ManagementAPI) detectExchangeRequest(event TapExchangeEvent) DetectResult {
	if api.inboundEngine != nil && event.RequestPreview != "" {
		return api.inboundEngine.DetectWithAppID(event.RequestPreview, event.ProjectID)
	}
	return DetectResult{Action: "pass"}
}

func (api *ManagementAPI) detectExchangeResponse(event TapExchangeEvent) DetectResult {
	if api.outboundEngine != nil && event.ResponsePreview != "" {
		result := api.outboundEngine.Detect(event.ResponsePreview)
		reasons := []string{}
		if result.Reason != "" {
			reasons = append(reasons, result.Reason)
		}
		return DetectResult{Action: result.Action, Reasons: reasons}
	}
	return DetectResult{Action: "pass"}
}

func (api *ManagementAPI) logExchangeAudit(direction string, event TapExchangeEvent, result DetectResult, durationMs float64) {
	if api.logger == nil {
		return
	}
	action := strings.TrimSpace(result.Action)
	if action == "" || action == "pass" {
		return
	}
	preview := event.RequestPreview
	if direction == "outbound" {
		preview = event.ResponsePreview
	}
	reason := strings.Join(result.Reasons, ",")
	if reason == "" && len(result.MatchedRules) > 0 {
		reason = strings.Join(result.MatchedRules, ",")
	}
	api.logger.LogWithTrace(direction, "", action, reason, preview, "", durationMs, event.UpstreamURL, event.ProjectID, event.TraceID)
}
