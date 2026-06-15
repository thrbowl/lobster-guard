package main

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"
	"time"
)

type databaseTableStat struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
}

func (api *ManagementAPI) handleSQLiteStats(w http.ResponseWriter, r *http.Request) {
	if api == nil || api.logger == nil || api.logger.DB() == nil {
		jsonResponse(w, 200, map[string]interface{}{"error": "database unavailable"})
		return
	}
	db := api.logger.DB()
	stats := db.Stats()
	result := map[string]interface{}{
		"database": map[string]interface{}{
			"url":            maskDatabaseURL(api.cfg.DatabaseURL),
			"open":           stats.OpenConnections,
			"in_use":         stats.InUse,
			"idle":           stats.Idle,
			"wait_count":     stats.WaitCount,
			"wait_duration":  stats.WaitDuration.String(),
			"max_open_conns": stats.MaxOpenConnections,
		},
		"tables":    []databaseTableStat{},
		"write_qps": 0.0,
	}

	tables, err := listDatabaseTables(db)
	if err == nil {
		result["table_count"] = len(tables)
		tableStats := make([]databaseTableStat, 0, len(tables))
		for _, name := range tables {
			count, qerr := countRows(db, name)
			if qerr != nil {
				continue
			}
			tableStats = append(tableStats, databaseTableStat{Name: name, Rows: count})
		}
		sort.Slice(tableStats, func(i, j int) bool {
			if tableStats[i].Rows == tableStats[j].Rows {
				return tableStats[i].Name < tableStats[j].Name
			}
			return tableStats[i].Rows > tableStats[j].Rows
		})
		if len(tableStats) > 10 {
			tableStats = tableStats[:10]
		}
		result["tables"] = tableStats
	}
	var recentWrites int64
	oneMinuteAgo := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339Nano)
	_ = db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE timestamp >= ?`, oneMinuteAgo).Scan(&recentWrites)
	result["recent_writes_1m"] = recentWrites
	result["write_qps"] = float64(recentWrites) / 60.0
	jsonResponse(w, 200, result)
}

func listDatabaseTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE' ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil && name != "" {
			tables = append(tables, name)
		}
	}
	return tables, nil
}

func countRows(db *sql.DB, table string) (int64, error) {
	var count int64
	query := `SELECT COUNT(*) FROM "` + strings.ReplaceAll(table, `"`, `""`) + `"`
	err := db.QueryRow(query).Scan(&count)
	return count, err
}
