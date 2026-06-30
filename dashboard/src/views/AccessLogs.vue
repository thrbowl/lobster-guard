<template>
  <div class="access-page">
    <div class="card">
      <div class="card-header">
        <span class="card-icon"><Icon name="activity" :size="18" /></span>
        <div>
          <span class="card-title">访问日志</span>
          <p class="card-subtitle">展示 OpenResty / exchange 记录的全量网关请求流水</p>
        </div>
        <div class="card-actions">
          <button class="btn btn-ghost btn-sm" @click="loadEvents" :disabled="loading"><Icon name="refresh-cw" :size="14" /> 刷新</button>
        </div>
      </div>

      <div class="access-filters">
        <div class="search-box">
          <Icon class="search-icon" name="search" :size="14" />
          <input v-model="filters.q" placeholder="搜索项目、Host、URI、请求/响应..." class="search-input" @keyup.enter="loadEvents" />
          <button v-if="filters.q" class="search-clear" @click="filters.q='';loadEvents()">x</button>
        </div>
        <select v-model="filters.project_id" class="filter-select" @change="loadEvents">
          <option value="">全部项目</option>
          <option v-for="p in projects" :key="p.project_id" :value="p.project_id">{{ p.project_name || p.project_id }}</option>
        </select>
        <select v-model="filters.mode" class="filter-select" @change="loadEvents">
          <option value="">全部模式</option>
          <option value="observe">observe</option>
          <option value="enforce">enforce</option>
        </select>
        <input v-model.number="filters.status" placeholder="状态码" class="filter-input status-input" @keyup.enter="loadEvents" />
        <button class="btn btn-sm" @click="loadEvents" :disabled="loading">筛选</button>
        <button class="btn btn-ghost btn-sm" @click="clearFilters">清除</button>
      </div>

      <DataTable :columns="columns" :data="events" :loading="loading" :page-size="20" :page-sizes="[20,50,100,200]" empty-text="暂无访问日志" empty-desc="请求通过 OpenResty exchange 后会出现在这里" :expandable="true" :row-class="rowClass" row-key="id">
        <template #cell-created_at="{row}"><span class="time-cell" :title="fullTime(row.created_at)">{{ relativeTime(row.created_at) }}</span></template>
        <template #cell-project_name="{row}"><span>{{ row.project_name || row.project_id || '--' }}</span></template>
        <template #cell-mode="{value}"><span class="tag" :class="value==='enforce'?'tag-block':'tag-observe'">{{ value || '--' }}</span></template>
        <template #cell-uri="{row}"><span class="uri-cell" :title="row.uri">{{ row.uri || '--' }}</span></template>
        <template #cell-response_status="{row}"><span class="mono" :class="statusClass(row.response_status)">{{ row.response_status || '--' }}</span></template>
        <template #cell-duration_ms="{row}"><span :class="durationClass(row.duration_ms)">{{ formatDuration(row.duration_ms) }}</span></template>
        <template #expand="{row}">
          <div class="detail">
            <div class="detail-grid">
              <div>
                <h4>接入信息</h4>
                <p><span>时间</span>{{ fullTime(row.created_at) }}</p>
                <p><span>项目</span>{{ row.project_name || row.project_id || '--' }}</p>
                <p><span>租户</span><code>{{ row.tenant_id || '--' }}</code></p>
              </div>
              <div>
                <h4>请求信息</h4>
                <p><span>方法</span><code>{{ row.method || '--' }}</code></p>
                <p><span>Host</span><code>{{ row.host || '--' }}</code></p>
                <p><span>URI</span><code>{{ row.uri || '--' }}</code></p>
                <p><span>上游</span><code>{{ row.upstream_url || '--' }}</code></p>
                <p><span>状态</span><code>{{ row.response_status || '--' }} / upstream {{ row.upstream_status || '--' }}</code></p>
              </div>
            </div>
            <div v-if="row.request_preview" class="preview"><h4>请求预览</h4><pre>{{ row.request_preview }}</pre></div>
            <div v-if="row.response_preview" class="preview"><h4>响应预览</h4><pre>{{ row.response_preview }}</pre></div>
          </div>
        </template>
      </DataTable>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api.js'
import { showToast } from '../stores/app.js'
import DataTable from '../components/DataTable.vue'
import Icon from '../components/Icon.vue'

const loading = ref(false)
const events = ref([])
const filters = reactive({ project_id:'', mode:'', status:'', q:'' })

const columns = [
  { key:'created_at', label:'时间', sortable:true, width:'150px' },
  { key:'project_name', label:'项目', sortable:true, width:'150px' },
  { key:'mode', label:'模式', sortable:true, width:'90px' },
  { key:'method', label:'方法', sortable:true, width:'80px' },
  { key:'host', label:'Host', sortable:true, width:'160px' },
  { key:'uri', label:'URI', sortable:false },
  { key:'response_status', label:'状态', sortable:true, width:'80px' },
  { key:'duration_ms', label:'耗时', sortable:true, width:'90px' },
]

const projects = computed(() => {
  const m = new Map()
  for (const e of events.value) {
    if (e.project_id && !m.has(e.project_id)) m.set(e.project_id, { project_id:e.project_id, project_name:e.project_name })
  }
  return [...m.values()]
})

function fullTime(ts) { if (!ts) return '--'; const d = new Date(ts); return isNaN(d.getTime()) ? String(ts) : d.toLocaleString('zh-CN', { hour12:false }) }
function relativeTime(ts) {
  if (!ts) return '--'
  const d = new Date(ts)
  if (isNaN(d.getTime())) return String(ts)
  const sec = Math.floor((Date.now() - d.getTime()) / 1000)
  if (sec < 60) return sec + '秒前'
  const min = Math.floor(sec / 60)
  if (min < 60) return min + '分钟前'
  const hr = Math.floor(min / 60)
  if (hr < 24) return hr + '小时前'
  return fullTime(ts)
}
function statusClass(status) { status = Number(status || 0); return status >= 500 ? 'bad' : status >= 400 ? 'warn' : '' }
function durationClass(ms) { ms = Number(ms || 0); return ms > 1000 ? 'bad' : ms > 300 ? 'warn' : '' }
function formatDuration(ms) { ms = Number(ms || 0); return ms ? ms.toFixed(ms >= 10 ? 0 : 1) + 'ms' : '--' }
function rowClass(row) {
  if (Number(row.response_status || 0) >= 500) return 'row-block'
  if (Number(row.response_status || 0) >= 400) return 'row-warn'
  return ''
}

async function loadEvents() {
  loading.value = true
  const p = ['limit=200']
  if (filters.project_id) p.push('project_id=' + encodeURIComponent(filters.project_id))
  if (filters.mode) p.push('mode=' + encodeURIComponent(filters.mode))
  if (filters.status) p.push('status=' + encodeURIComponent(filters.status))
  if (filters.q) p.push('q=' + encodeURIComponent(filters.q))
  try {
    const d = await api('/api/v1/tap-exchange-events?' + p.join('&'))
    events.value = d.events || []
  } catch (e) {
    events.value = []
    showToast('加载访问日志失败: ' + e.message, 'error')
  } finally {
    loading.value = false
  }
}
function clearFilters() {
  filters.project_id = ''
  filters.mode = ''
  filters.status = ''
  filters.q = ''
  loadEvents()
}

onMounted(loadEvents)
</script>

<style scoped>
.access-page { display:flex; flex-direction:column; gap:var(--space-4); }
.card-subtitle { margin:4px 0 0; color:var(--text-tertiary); font-size:var(--text-xs); }
.access-filters { display:flex; align-items:center; gap:var(--space-2); flex-wrap:wrap; margin-bottom:var(--space-3); }
.search-box { position:relative; flex:1; min-width:240px; max-width:460px; }
.search-icon { position:absolute; left:10px; top:50%; transform:translateY(-50%); color:var(--text-tertiary); pointer-events:none; }
.search-input { width:100%; padding:var(--space-2) var(--space-3) var(--space-2) 32px; background:var(--bg-elevated); border:1px solid var(--border-default); border-radius:var(--radius-md); color:var(--text-primary); font-size:var(--text-sm); outline:none; }
.search-clear { position:absolute; right:8px; top:50%; transform:translateY(-50%); background:none; border:0; color:var(--text-tertiary); cursor:pointer; }
.filter-select, .filter-input { background:var(--bg-elevated); border:1px solid var(--border-default); border-radius:var(--radius-md); color:var(--text-primary); padding:var(--space-2) var(--space-3); font-size:var(--text-sm); outline:none; }
.mono-input { font-family:var(--font-mono); }
.status-input { width:90px; }
.time-cell, .mono, code { font-family:var(--font-mono); font-size:var(--text-xs); }
.uri-cell { max-width:360px; overflow:hidden; text-overflow:ellipsis; display:inline-block; }
.tag { display:inline-block; padding:1px 8px; border-radius:999px; font-size:var(--text-xs); font-weight:600; white-space:nowrap; }
.tag-block { background:rgba(239,68,68,.15); color:#ef4444; }
.tag-warn { background:rgba(245,158,11,.15); color:#f59e0b; }
.tag-pass { background:rgba(16,185,129,.15); color:#10b981; }
.tag-log { background:rgba(107,114,128,.15); color:#9ca3af; }
.tag-observe { background:rgba(59,130,246,.15); color:#3b82f6; }
.bad { color:#ef4444; font-weight:600; }
.warn { color:#f59e0b; }
:deep(.row-block) { background:rgba(239,68,68,.04) !important; }
:deep(.row-warn) { background:rgba(245,158,11,.04) !important; }
.detail { background:var(--bg-elevated); padding:var(--space-4); border-radius:var(--radius-md); font-size:var(--text-sm); }
.detail-grid { display:grid; grid-template-columns:1fr 1fr; gap:var(--space-4); }
.detail h4 { margin:0 0 var(--space-2); color:var(--color-primary); font-size:var(--text-xs); text-transform:uppercase; letter-spacing:.05em; }
.detail p { display:flex; gap:var(--space-2); margin:4px 0; }
.detail p span { min-width:60px; color:var(--text-tertiary); font-size:var(--text-xs); }
.preview { margin-top:var(--space-3); }
.preview pre { margin:0; padding:var(--space-3); background:var(--bg-base); border-radius:var(--radius-sm); overflow:auto; max-height:260px; white-space:pre-wrap; word-break:break-word; }
@media (max-width:700px) { .detail-grid { grid-template-columns:1fr; } }
</style>
