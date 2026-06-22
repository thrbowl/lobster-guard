<template>
  <div>
    <div class="card" style="margin-bottom:20px">
      <div class="card-header"><span class="card-icon">🗄️</span><span class="card-title">数据库状态</span><div class="card-actions"><button class="btn btn-ghost btn-sm" @click="loadSQLiteStats">刷新</button></div></div>
      <Skeleton v-if="sqliteStatsLoading && !sqliteStats" type="text" />
      <div v-else-if="sqliteStats" class="sqlite-panel">
        <div class="sqlite-overview">
          <div class="sqlite-stat-card">
            <div class="sqlite-stat-label">连接池</div>
            <div class="sqlite-stat-value">{{ sqliteStats.database?.open ?? '--' }}</div>
            <div class="sqlite-stat-sub">in_use={{ sqliteStats.database?.in_use ?? 0 }} idle={{ sqliteStats.database?.idle ?? 0 }}</div>
          </div>
          <div class="sqlite-stat-card">
            <div class="sqlite-stat-label">等待次数</div>
            <div class="sqlite-stat-value">{{ sqliteStats.database?.wait_count ?? 0 }}</div>
            <div class="sqlite-stat-sub">{{ sqliteStats.database?.wait_duration || '0s' }}</div>
          </div>
          <div class="sqlite-stat-card">
            <div class="sqlite-stat-label">表数量</div>
            <div class="sqlite-stat-value">{{ sqliteStats.table_count ?? '--' }}</div>
            <div class="sqlite-stat-sub">{{ sqliteStats.database?.url || '' }}</div>
          </div>
          <div class="sqlite-stat-card">
            <div class="sqlite-stat-label">最近写入 QPS</div>
            <div class="sqlite-stat-value">{{ formatQPS(sqliteStats.write_qps) }}</div>
            <div class="sqlite-stat-sub">1 分钟 {{ sqliteStats.recent_writes_1m || 0 }} 次</div>
          </div>
        </div>

        <div class="card">
          <div class="card-header"><span class="card-icon">📊</span><span class="card-title">Top 10 表行数</span></div>
          <div v-if="!(sqliteStats.tables || []).length" class="empty" style="padding:24px">暂无表数据</div>
          <div v-else class="sqlite-bars">
            <div v-for="table in sqliteStats.tables" :key="table.name" class="sqlite-bar-row">
              <div class="sqlite-bar-head">
                <span class="sqlite-bar-name">{{ table.name }}</span>
                <span class="sqlite-bar-value">{{ table.rows }}</span>
              </div>
              <div class="sqlite-bar-track">
                <div class="sqlite-bar-fill" :style="{ width: sqliteBarWidth(table.rows) + '%' }"></div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import Skeleton from '../../components/Skeleton.vue'

defineProps({
  sqliteStatsLoading: Boolean,
  sqliteStats: Object,
  loadSQLiteStats: Function,
  formatQPS: Function,
  sqliteBarWidth: Function,
})
</script>

<style scoped>
.status-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 8px 0; border-bottom: 1px solid var(--border-subtle); }
.status-row:last-child { border-bottom: none; }
.status-key { color: var(--text-secondary); font-size: var(--text-sm); }
.status-val { color: var(--text-primary); font-size: var(--text-sm); }
.sqlite-panel { display: flex; flex-direction: column; gap: 16px; }
.sqlite-overview { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; margin-bottom: 4px; }
.sqlite-stat-card { background: var(--bg-elevated); border: 1px solid var(--border-subtle); border-radius: var(--radius-md); padding: 16px; }
.sqlite-stat-label { font-size: var(--text-xs); color: var(--text-tertiary); margin-bottom: 6px; }
.sqlite-stat-value { font-size: 1.4rem; font-weight: 700; color: var(--text-primary); }
.sqlite-stat-sub { font-size: var(--text-xs); color: var(--text-secondary); margin-top: 6px; font-family: var(--font-mono); }
.sqlite-bars { display: flex; flex-direction: column; gap: 12px; }
.sqlite-bar-row { display: flex; flex-direction: column; gap: 6px; }
.sqlite-bar-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; font-size: var(--text-sm); }
.sqlite-bar-name { color: var(--text-primary); font-family: var(--font-mono); }
.sqlite-bar-value { color: var(--text-secondary); font-weight: 600; }
.sqlite-bar-track { height: 10px; background: var(--bg-elevated); border-radius: 9999px; overflow: hidden; border: 1px solid var(--border-subtle); }
.sqlite-bar-fill { height: 100%; background: linear-gradient(90deg, var(--color-primary), #22c55e); border-radius: 9999px; }
</style>
