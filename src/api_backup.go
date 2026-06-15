package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

func (api *ManagementAPI) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, 501, map[string]string{"error": "PostgreSQL 备份请使用 pg_dump，应用内文件级备份已禁用"})
}

// handleListBackups GET /api/v1/backups — 列出已有备份
func (api *ManagementAPI) handleListBackups(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, 200, map[string]interface{}{
		"backups": []BackupInfo{},
		"total":   0,
		"note":    "PostgreSQL 备份请使用 pg_dump/pg_restore",
	})
}

// handleDeleteBackup DELETE /api/v1/backups/:name — 删除指定备份
func (api *ManagementAPI) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/backups/")
	if name == "" {
		jsonResponse(w, 400, map[string]string{"error": "backup name required"})
		return
	}
	log.Printf("[备份] PostgreSQL 内置删除备份请求被拒绝: %s", name)
	jsonResponse(w, 501, map[string]string{"error": "PostgreSQL 备份请使用 pg_dump/pg_restore 管理"})
}

// ============================================================
// v5.0 实时监控 API
// ============================================================

// handleRealtimeMetrics GET /api/v1/metrics/realtime — 返回最近 60 秒逐秒统计
func (api *ManagementAPI) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	// Extract name: /api/v1/backups/{name}/restore
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/backups/")
	name := strings.TrimSuffix(path, "/restore")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		jsonResponse(w, 400, map[string]string{"error": "invalid backup name"})
		return
	}
	jsonResponse(w, 501, map[string]string{"error": "PostgreSQL 恢复请使用 pg_restore 后重启服务"})
}

// handleDownloadBackup GET /api/v1/backups/:name/download — 下载备份文件
func (api *ManagementAPI) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/backups/")
	name := strings.TrimSuffix(path, "/download")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		jsonResponse(w, 400, map[string]string{"error": "invalid backup name"})
		return
	}
	jsonResponse(w, 501, map[string]string{"error": "PostgreSQL 备份下载请通过外部 pg_dump 产物管理"})
}

// formatBytes 格式化字节数为可读字符串
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// ============================================================
// v9.0 LLM 侧安全审计 API
// ============================================================

// handleLLMStatus GET /api/v1/llm/status — LLM 代理状态
