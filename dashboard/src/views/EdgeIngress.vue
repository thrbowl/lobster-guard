<template>
  <div class="edge-page">
    <div class="page-header">
      <div>
        <h1 class="page-title"><Icon name="door" :size="20" /> 边缘接入</h1>
        <p class="page-subtitle">管理项目入口、URI 规则，并生成 OpenResty 使用的 edge-routes.json</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-ghost btn-sm" @click="loadAll" :disabled="loading"><Icon name="refresh" :size="14" /> 刷新</button>
        <button class="btn btn-sm" @click="openProjectCreate"><Icon name="plus" :size="14" /> 新建项目</button>
        <button class="btn btn-primary btn-sm" @click="syncRoutes" :disabled="syncing"><Icon name="save" :size="14" /> {{ syncing ? '生成中...' : '同步到 OpenResty' }}</button>
      </div>
    </div>

    <div class="stats-row">
      <div class="stat-box"><span>项目</span><strong>{{ projects.length }}</strong></div>
      <div class="stat-box"><span>启用项目</span><strong>{{ enabledProjects.length }}</strong></div>
      <div class="stat-box"><span>URI 规则</span><strong>{{ routes.length }}</strong></div>
      <div class="stat-box"><span>启用规则</span><strong>{{ enabledRouteCount }}</strong></div>
      <div class="stat-box"><span>Observe</span><strong>{{ observeCount }}</strong></div>
      <div class="stat-box"><span>Enforce</span><strong>{{ enforceCount }}</strong></div>
    </div>

    <div v-if="syncResult" class="sync-inline">
      <span>已同步</span>
      <strong class="td-mono">{{ syncResult.path }}</strong>
      <span>版本 {{ syncResult.version }}</span>
      <span>{{ syncResult.total }} 条路由</span>
    </div>

    <div class="tab-bar">
      <button class="tab-btn" :class="{ active: activeTab === 'projects' }" @click="activeTab = 'projects'"><Icon name="building" :size="14" /> 项目管理</button>
      <button class="tab-btn" :class="{ active: activeTab === 'routes' }" @click="activeTab = 'routes'"><Icon name="git-branch" :size="14" /> URI 规则</button>
    </div>

    <section v-if="activeTab === 'projects'" class="section">
      <div class="section-head">
        <div>
          <h2>项目管理</h2>
          <p>项目是接入、路由、审计和统计的稳定归属，不等同于租户。</p>
        </div>
        <button class="btn btn-sm" @click="openProjectCreate"><Icon name="plus" :size="14" /> 新建项目</button>
      </div>
      <div class="table-wrap">
        <table class="data-table">
          <thead>
            <tr><th>名称</th><th>入口 Host</th><th>Upstream URL</th><th>转发策略</th><th>默认模式</th><th>租户</th><th>状态</th><th>备注</th><th>创建时间</th><th class="td-actions">操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="p in sortedProjects" :key="p.id" :class="{ 'row-off': !p.enabled }">
              <td><div>{{ p.name }}</div><small class="muted td-mono">{{ p.id }}</small></td>
              <td class="td-mono">{{ p.hosts?.[0] || '-' }}</td>
              <td class="td-mono url-cell" :title="p.upstream_url">{{ p.upstream_url || '-' }}</td>
              <td class="td-mono">{{ p.host_policy || 'upstream_host' }}</td>
              <td><span class="badge" :class="p.default_mode === 'enforce' ? 'badge-warn' : 'badge-info'">{{ p.default_mode || 'observe' }}</span></td>
              <td class="td-mono">{{ p.tenant_id || 'default' }}</td>
              <td><span class="badge" :class="p.enabled ? 'badge-ok' : 'badge-off'">{{ p.enabled ? '启用' : '禁用' }}</span></td>
              <td class="muted">{{ p.description || '-' }}</td>
              <td class="td-mono muted">{{ formatTime(p.created_at) }}</td>
              <td class="td-actions">
                <button class="btn-icon" @click="openProjectEdit(p)" title="编辑"><Icon name="edit" :size="14" /></button>
                <button class="btn-icon" @click="toggleProject(p)" :title="p.enabled ? '禁用' : '启用'"><Icon :name="p.enabled ? 'x-circle' : 'check-circle'" :size="14" /></button>
                <button class="btn-icon btn-icon-danger" @click="deleteProject(p)" title="删除"><Icon name="trash" :size="14" /></button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="!projects.length" class="empty-state">暂无项目，先创建一个接入项目</div>
      </div>
    </section>

    <section v-if="activeTab === 'routes'" class="section">
      <div class="section-head">
        <div>
          <h2>URI 规则</h2>
          <p>URI 规则只覆盖项目默认模式，不进入 OpenResty 生成文件。</p>
        </div>
        <button class="btn btn-sm" @click="openRouteCreate" :disabled="enabledProjects.length === 0"><Icon name="plus" :size="14" /> 新建规则</button>
      </div>
      <div class="filter-bar">
        <div class="search-box">
          <Icon name="search" :size="14" class="search-icon" />
          <input v-model="routeSearch" class="search-input" placeholder="搜索项目 / URI..." />
          <button v-if="routeSearch" class="search-clear" @click="routeSearch = ''">x</button>
        </div>
        <select v-model="routeProjectFilter" class="field-select small-select">
          <option value="">全部项目</option>
          <option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option>
        </select>
        <select v-model="routeModeFilter" class="field-select small-select">
          <option value="">全部模式</option>
          <option value="observe">observe</option>
          <option value="enforce">enforce</option>
        </select>
      </div>
      <div class="table-wrap">
        <table class="data-table">
          <thead>
            <tr><th>项目</th><th>URI</th><th>匹配方式</th><th>模式</th><th>优先级</th><th>状态</th><th class="td-actions">操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="r in filteredRoutes" :key="r.id" :class="{ 'row-off': !r.enabled }">
              <td><div>{{ r.project_name || r.project_id }}</div><small class="muted td-mono">{{ r.project_id }}</small></td>
              <td class="td-mono">{{ r.path_prefix }}</td>
              <td class="td-mono">{{ r.match_type || 'prefix' }}</td>
              <td><span class="badge" :class="r.mode === 'enforce' ? 'badge-warn' : 'badge-info'">{{ r.mode }}</span></td>
              <td>{{ r.priority }}</td>
              <td><span class="badge" :class="r.enabled ? 'badge-ok' : 'badge-off'">{{ r.enabled ? '启用' : '禁用' }}</span></td>
              <td class="td-actions">
                <button class="btn-icon" @click="openRouteEdit(r)" title="编辑"><Icon name="edit" :size="14" /></button>
                <button class="btn-icon" @click="toggleRoute(r)" :title="r.enabled ? '禁用' : '启用'"><Icon :name="r.enabled ? 'x-circle' : 'check-circle'" :size="14" /></button>
                <button class="btn-icon btn-icon-danger" @click="deleteRoute(r)" title="删除"><Icon name="trash" :size="14" /></button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="routes.length && !filteredRoutes.length" class="empty-state">没有匹配的规则</div>
        <div v-if="!routes.length" class="empty-state">暂无 URI 规则，未命中的请求使用项目默认模式</div>
      </div>
    </section>

    <Transition name="fade">
      <div v-if="projectDialog" class="dialog-overlay" @click.self="projectDialog = false">
        <div class="dialog">
          <div class="dialog-header"><span>{{ editingProject ? '编辑项目' : '新建项目' }}</span><button class="dlg-close" @click="projectDialog = false">x</button></div>
          <div class="dialog-body">
            <template v-if="editingProject">
              <label class="field-label">项目 ID</label>
              <input v-model.trim="projectForm.id" class="field-input" disabled />
            </template>
            <label class="field-label">项目名称 <span class="req">*</span></label>
            <input v-model.trim="projectForm.name" class="field-input" placeholder="OpenClaw 蓝信测试" />
            <div class="project-route-grid">
              <div>
                <label class="field-label">入口 Host <span class="req">*</span></label>
                <input v-model.trim="projectForm.host" class="field-input" placeholder="openclaw.example.com" />
              </div>
              <div>
                <label class="field-label">默认模式</label>
                <select v-model="projectForm.default_mode" class="field-select"><option value="observe">observe</option><option value="enforce">enforce</option></select>
              </div>
            </div>
            <label class="field-label">Upstream URL <span class="req">*</span></label>
            <input v-model.trim="projectForm.upstream_url" class="field-input" placeholder="http://127.0.0.1:18790" />
            <label class="field-label">Host 转发策略</label>
            <select v-model="projectForm.host_policy" class="field-select"><option value="upstream_host">upstream_host</option><option value="preserve">preserve</option></select>
            <label class="field-label">租户 ID</label>
            <input v-model.trim="projectForm.tenant_id" class="field-input" placeholder="default" />
            <label class="field-label">备注</label>
            <textarea v-model.trim="projectForm.description" class="field-input textarea" rows="3" placeholder="客户现场接入测试"></textarea>
            <label class="toggle-row"><input type="checkbox" v-model="projectForm.enabled" /> <span>启用项目</span></label>
          </div>
          <div class="dialog-footer">
            <button class="btn btn-sm" @click="projectDialog = false">取消</button>
            <button class="btn btn-primary btn-sm" @click="saveProject" :disabled="saving">保存</button>
          </div>
        </div>
      </div>
    </Transition>

    <Transition name="fade">
      <div v-if="routeDialog" class="dialog-overlay" @click.self="routeDialog = false">
        <div class="dialog dialog-wide">
          <div class="dialog-header"><span>{{ editingRoute ? '编辑 URI 规则' : '新建 URI 规则' }}</span><button class="dlg-close" @click="routeDialog = false">x</button></div>
          <div class="dialog-body route-form-grid">
            <div>
              <label class="field-label">项目 <span class="req">*</span></label>
              <select v-model="routeForm.project_id" class="field-select">
                <option value="">选择项目</option>
                <option v-for="p in enabledProjects" :key="p.id" :value="p.id">{{ p.name }} / {{ p.id }}</option>
              </select>
            </div>
            <div>
              <label class="field-label">匹配方式</label>
              <select v-model="routeForm.match_type" class="field-select"><option value="prefix">prefix</option><option value="exact">exact</option></select>
            </div>
            <div class="form-span uri-field">
              <label class="field-label">URI <span class="req">*</span></label>
              <input
                v-model.trim="routeForm.path_prefix"
                class="field-input"
                placeholder="/api/chat"
                @focus="uriInputFocused = true"
                @blur="hideUriSuggestions"
              />
              <div v-if="showUriSuggestions" class="uri-suggestions">
                <button
                  v-for="uri in filteredUriSuggestions"
                  :key="uri"
                  type="button"
                  class="uri-suggestion"
                  @mousedown.prevent="selectUri(uri)"
                >{{ uri }}</button>
                <div v-if="loadingUris" class="uri-suggestion muted">加载最近访问 URI...</div>
                <div v-else-if="!filteredUriSuggestions.length" class="uri-suggestion muted">暂无匹配 URI</div>
              </div>
              <small class="field-help">默认展示该项目最近唯一的 10 条访问 URI；输入 3 个字符后自动过滤。</small>
            </div>
            <div>
              <label class="field-label">模式</label>
              <select v-model="routeForm.mode" class="field-select"><option value="observe">observe</option><option value="enforce">enforce</option></select>
            </div>
            <div>
              <label class="field-label">优先级</label>
              <input v-model.number="routeForm.priority" type="number" class="field-input" />
            </div>
            <div class="form-span">
              <label class="field-label">备注</label>
              <textarea v-model.trim="routeForm.description" class="field-input textarea" rows="2" placeholder="OpenClaw + 蓝信 observe 试点"></textarea>
            </div>
            <div class="form-span">
              <label class="toggle-row"><input type="checkbox" v-model="routeForm.enabled" /> <span>启用规则</span></label>
            </div>
          </div>
          <div class="dialog-footer">
            <button class="btn btn-sm" @click="routeDialog = false">取消</button>
            <button class="btn btn-primary btn-sm" @click="saveRoute" :disabled="saving">保存</button>
          </div>
        </div>
      </div>
    </Transition>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api, apiPost, apiPut, apiDelete } from '../api.js'
import { showToast } from '../stores/app.js'
import Icon from '../components/Icon.vue'

const activeTab = ref('projects')
const loading = ref(false)
const saving = ref(false)
const syncing = ref(false)
const projects = ref([])
const routes = ref([])
const syncResult = ref(null)

const projectDialog = ref(false)
const editingProject = ref(null)
const projectForm = reactive({
  id: '', name: '', tenant_id: 'default', description: '', enabled: true,
  host: '', default_mode: 'observe', upstream_url: '', host_policy: 'upstream_host'
})

const routeDialog = ref(false)
const editingRoute = ref(null)
const routeForm = reactive({
  id: '', project_id: '', path_prefix: '', match_type: 'prefix', mode: 'observe',
  enabled: true, priority: 100, description: ''
})
const uriSuggestions = ref([])
const uriInputFocused = ref(false)
const loadingUris = ref(false)

const routeSearch = ref('')
const routeProjectFilter = ref('')
const routeModeFilter = ref('')

const enabledProjects = computed(() => projects.value.filter(p => p.enabled))
const sortedProjects = computed(() => [...projects.value].sort((a, b) => {
  const at = new Date(a.created_at || 0).getTime()
  const bt = new Date(b.created_at || 0).getTime()
  return bt - at
}))
const enabledRouteCount = computed(() => routes.value.filter(r => r.enabled).length)
const observeCount = computed(() => routes.value.filter(r => r.mode === 'observe').length)
const enforceCount = computed(() => routes.value.filter(r => r.mode === 'enforce').length)
const filteredRoutes = computed(() => {
  const q = routeSearch.value.trim().toLowerCase()
  return routes.value.filter(r => {
    if (routeProjectFilter.value && r.project_id !== routeProjectFilter.value) return false
    if (routeModeFilter.value && r.mode !== routeModeFilter.value) return false
    if (!q) return true
    return [
      r.project_id, r.project_name, r.path_prefix, r.match_type
    ].filter(Boolean).some(v => String(v).toLowerCase().includes(q))
  })
})
const filteredUriSuggestions = computed(() => {
  const input = routeForm.path_prefix.trim().toLowerCase()
  if (input.length < 3) return uriSuggestions.value.slice(0, 10)
  return uriSuggestions.value.filter(uri => uri.toLowerCase().includes(input)).slice(0, 10)
})
const showUriSuggestions = computed(() => uriInputFocused.value && routeDialog.value)

async function loadAll() {
  loading.value = true
  try {
    const [p, r] = await Promise.all([
      api('/api/v1/edge-projects'),
      api('/api/v1/edge-routes')
    ])
    projects.value = p.projects || []
    routes.value = r.routes || []
  } catch (e) {
    showToast('加载失败: ' + e.message, 'error')
  } finally {
    loading.value = false
  }
}

function openProjectCreate() {
  editingProject.value = null
  Object.assign(projectForm, {
    id: '', name: '', tenant_id: 'default', description: '', enabled: true,
    host: '', default_mode: 'observe', upstream_url: '', host_policy: 'upstream_host'
  })
  projectDialog.value = true
}

function openProjectEdit(project) {
  editingProject.value = project
  Object.assign(projectForm, {
    id: project.id, name: project.name, tenant_id: project.tenant_id || 'default',
    description: project.description || '', enabled: project.enabled !== false,
    host: project.hosts?.[0] || '', default_mode: project.default_mode || 'observe',
    upstream_url: project.upstream_url || '', host_policy: project.host_policy || 'upstream_host'
  })
  projectDialog.value = true
}

async function saveProject() {
  if (!projectForm.name) { showToast('项目名称必填', 'warning'); return }
  if (!projectForm.host || !projectForm.upstream_url) {
    showToast('入口 Host 和 Upstream URL 必填', 'warning')
    return
  }
  saving.value = true
  const body = {
    id: editingProject.value ? projectForm.id : generateProjectID(),
    name: projectForm.name,
    tenant_id: projectForm.tenant_id || 'default',
    hosts: [projectForm.host],
    upstream_url: projectForm.upstream_url,
    host_policy: projectForm.host_policy || 'upstream_host',
    default_mode: projectForm.default_mode || 'observe',
    description: projectForm.description,
    enabled: projectForm.enabled,
  }
  try {
    if (editingProject.value) {
      await apiPut('/api/v1/edge-projects/' + encodeURIComponent(projectForm.id), body)
    } else {
      await apiPost('/api/v1/edge-projects', body)
    }
    projectDialog.value = false
    showToast(editingProject.value ? '项目已保存' : '项目已创建', 'success')
    await loadAll()
  } catch (e) {
    showToast('保存失败: ' + e.message, 'error')
  } finally {
    saving.value = false
  }
}

function generateProjectID() {
  if (crypto?.randomUUID) return 'ep_' + crypto.randomUUID()
  return 'ep_' + Date.now().toString(36) + '_' + Math.random().toString(36).slice(2)
}

function generateRouteID() {
  if (crypto?.randomUUID) return 'er_' + crypto.randomUUID()
  return 'er_' + Date.now().toString(36) + '_' + Math.random().toString(36).slice(2)
}

async function toggleProject(project) {
  try {
    await apiPut('/api/v1/edge-projects/' + encodeURIComponent(project.id), { ...project, enabled: !project.enabled })
    showToast(project.enabled ? '项目已禁用' : '项目已启用', 'success')
    await loadAll()
  } catch (e) {
    showToast('操作失败: ' + e.message, 'error')
  }
}

async function deleteProject(project) {
  if (!confirm(`确定删除项目 ${project.name}？如果项目下仍有路由，后端会拒绝删除。`)) return
  try {
    await apiDelete('/api/v1/edge-projects/' + encodeURIComponent(project.id))
    showToast('项目已删除', 'success')
    await loadAll()
  } catch (e) {
    showToast('删除失败: ' + e.message, 'error')
  }
}

function openRouteCreate() {
  editingRoute.value = null
  Object.assign(routeForm, {
    id: '', project_id: enabledProjects.value[0]?.id || '', path_prefix: '',
    match_type: 'prefix', mode: 'observe', enabled: true, priority: 100, description: ''
  })
  loadUriSuggestions(routeForm.project_id)
  routeDialog.value = true
}

function openRouteEdit(route) {
  editingRoute.value = route
  Object.assign(routeForm, {
    id: route.id, project_id: route.project_id, path_prefix: route.path_prefix || '/',
    match_type: route.match_type || 'prefix', mode: route.mode || 'observe', enabled: route.enabled !== false,
    priority: route.priority || 100, description: route.description || ''
  })
  loadUriSuggestions(routeForm.project_id)
  routeDialog.value = true
}

function routeBody() {
  return {
    id: routeForm.id,
    project_id: routeForm.project_id,
    path_prefix: normalizeRouteURI(routeForm.path_prefix),
    match_type: routeForm.match_type || 'prefix',
    mode: routeForm.mode,
    enabled: routeForm.enabled,
    priority: Number(routeForm.priority) || 100,
    description: routeForm.description
  }
}

async function saveRoute() {
  const body = routeBody()
  if (!body.project_id || !body.path_prefix) {
    showToast('项目和 URI 必填', 'warning')
    return
  }
  if (body.path_prefix === '/') {
    showToast('/ 是项目默认兜底入口，请填写具体业务 URI', 'warning')
    return
  }
  if (!editingRoute.value) body.id = generateRouteID()
  saving.value = true
  try {
    if (editingRoute.value) await apiPut('/api/v1/edge-routes/' + encodeURIComponent(body.id), body)
    else await apiPost('/api/v1/edge-routes', body)
    routeDialog.value = false
    showToast('URI 规则已保存', 'success')
    await loadAll()
  } catch (e) {
    showToast('保存失败: ' + e.message, 'error')
  } finally {
    saving.value = false
  }
}

function normalizeRouteURI(value) {
  let uri = String(value || '').trim()
  if (!uri) return '/'
  try {
    if (/^https?:\/\//i.test(uri)) uri = new URL(uri).pathname || '/'
  } catch {}
  const queryIndex = uri.indexOf('?')
  if (queryIndex >= 0) uri = uri.slice(0, queryIndex)
  if (!uri.startsWith('/')) uri = '/' + uri
  return uri || '/'
}

function normalizeLogURI(value) {
  const uri = normalizeRouteURI(value)
  return uri || '/'
}

function selectUri(uri) {
  routeForm.path_prefix = uri
  uriInputFocused.value = false
}

function hideUriSuggestions() {
  window.setTimeout(() => { uriInputFocused.value = false }, 120)
}

async function loadUriSuggestions(projectID) {
  uriSuggestions.value = []
  if (!projectID) return
  loadingUris.value = true
  try {
    const data = await api('/api/v1/tap-exchange-events?project_id=' + encodeURIComponent(projectID) + '&limit=200')
    const seen = new Set()
    const list = []
    for (const event of data.events || []) {
      const uri = normalizeLogURI(event.uri)
      if (!uri || seen.has(uri)) continue
      seen.add(uri)
      list.push(uri)
      if (list.length >= 10) break
    }
    uriSuggestions.value = list
  } catch (e) {
    uriSuggestions.value = []
  } finally {
    loadingUris.value = false
  }
}

watch(() => routeForm.project_id, projectID => {
  if (routeDialog.value) loadUriSuggestions(projectID)
})

async function toggleRoute(route) {
  try {
    await apiPut('/api/v1/edge-routes/' + encodeURIComponent(route.id), { ...route, enabled: !route.enabled })
    showToast(route.enabled ? '规则已禁用' : '规则已启用', 'success')
    await loadAll()
  } catch (e) {
    showToast('操作失败: ' + e.message, 'error')
  }
}

async function deleteRoute(route) {
  if (!confirm(`确定删除 URI 规则 ${route.path_prefix}？`)) return
  try {
    await apiDelete('/api/v1/edge-routes/' + encodeURIComponent(route.id))
    showToast('URI 规则已删除', 'success')
    await loadAll()
  } catch (e) {
    showToast('删除失败: ' + e.message, 'error')
  }
}

async function syncRoutes() {
  syncing.value = true
  try {
    const data = await apiPost('/api/v1/edge-routes/sync', {})
    syncResult.value = data.result
    showToast('edge-routes.json 已同步生成', 'success')
  } catch (e) {
    showToast('同步失败: ' + e.message, 'error')
  } finally {
    syncing.value = false
  }
}

function formatTime(value) {
  if (!value) return '-'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  return d.toLocaleString()
}

onMounted(loadAll)
</script>

<style scoped>
.edge-page { display: flex; flex-direction: column; gap: var(--space-4); }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--space-4); }
.page-title { display: flex; align-items: center; gap: var(--space-2); margin: 0; color: var(--text-primary); font-size: 1.35rem; }
.page-subtitle { margin: 6px 0 0; color: var(--text-secondary); font-size: var(--text-sm); }
.header-actions { display: flex; align-items: center; gap: var(--space-2); flex-wrap: wrap; justify-content: flex-end; }
.stats-row { display: grid; grid-template-columns: repeat(6, minmax(120px, 1fr)); gap: var(--space-3); }
.stat-box { background: var(--bg-surface); border: 1px solid var(--border-subtle); border-radius: var(--radius-md); padding: 14px 16px; display: flex; flex-direction: column; gap: 6px; }
.stat-box span { color: var(--text-tertiary); font-size: var(--text-xs); }
.stat-box strong { color: var(--text-primary); font-size: 1.25rem; }
.tab-bar { display: flex; gap: 6px; border-bottom: 1px solid var(--border-subtle); }
.tab-btn { display: inline-flex; align-items: center; gap: 6px; padding: 10px 14px; border: 0; background: transparent; color: var(--text-secondary); cursor: pointer; border-bottom: 2px solid transparent; }
.tab-btn.active { color: var(--color-primary); border-bottom-color: var(--color-primary); }
.section { background: var(--bg-surface); border: 1px solid var(--border-subtle); border-radius: var(--radius-md); overflow: hidden; }
.section-head { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--space-4); padding: var(--space-4); border-bottom: 1px solid var(--border-subtle); }
.section-head h2 { margin: 0; font-size: 1rem; color: var(--text-primary); }
.section-head p { margin: 6px 0 0; color: var(--text-tertiary); font-size: var(--text-xs); }
.filter-bar { display: flex; gap: var(--space-2); align-items: center; padding: var(--space-3) var(--space-4); border-bottom: 1px solid var(--border-subtle); }
.search-box { position: relative; flex: 1; min-width: 220px; }
.search-icon { position: absolute; left: 10px; top: 50%; transform: translateY(-50%); color: var(--text-tertiary); }
.search-input { width: 100%; padding: 8px 34px; background: var(--bg-elevated); border: 1px solid var(--border-subtle); border-radius: var(--radius-md); color: var(--text-primary); }
.search-clear { position: absolute; right: 8px; top: 50%; transform: translateY(-50%); border: 0; background: transparent; color: var(--text-tertiary); cursor: pointer; }
.small-select { width: 150px; }
.table-wrap { overflow-x: auto; }
.data-table { width: 100%; border-collapse: collapse; }
.data-table th, .data-table td { padding: 11px 12px; border-bottom: 1px solid var(--border-subtle); text-align: left; vertical-align: middle; font-size: var(--text-sm); }
.data-table th { color: var(--text-tertiary); font-size: var(--text-xs); font-weight: 700; background: var(--bg-elevated); }
.row-off { opacity: .58; }
.td-mono { font-family: var(--font-mono); font-size: .78rem; }
.td-actions { text-align: right; white-space: nowrap; width: 128px; }
.muted { color: var(--text-tertiary); }
.url-cell { max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.badge { display: inline-flex; align-items: center; padding: 3px 8px; border-radius: 999px; font-size: .72rem; font-weight: 700; }
.badge-ok { background: rgba(34,197,94,.14); color: #22c55e; }
.badge-off { background: rgba(148,163,184,.14); color: var(--text-tertiary); }
.badge-info { background: rgba(59,130,246,.14); color: #60a5fa; }
.badge-warn { background: rgba(245,158,11,.14); color: #f59e0b; }
.host-pill { display: inline-flex; margin: 2px 4px 2px 0; padding: 3px 7px; border-radius: 6px; background: var(--color-primary-dim); color: var(--color-primary); font-family: var(--font-mono); font-size: .72rem; }
.btn-icon { display: inline-flex; align-items: center; justify-content: center; width: 30px; height: 30px; border-radius: var(--radius-md); border: 1px solid transparent; background: transparent; color: var(--text-secondary); cursor: pointer; }
.btn-icon:hover { background: var(--bg-elevated); color: var(--text-primary); }
.btn-icon-danger:hover { color: var(--color-danger); }
.empty-state { padding: 28px; text-align: center; color: var(--text-tertiary); }
.sync-inline { display: flex; align-items: center; gap: var(--space-3); padding: 10px 12px; background: rgba(34,197,94,.08); border: 1px solid rgba(34,197,94,.22); border-radius: var(--radius-md); color: var(--text-secondary); font-size: var(--text-xs); }
.sync-inline strong { color: var(--text-primary); max-width: 520px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.dialog-overlay { position: fixed; inset: 0; background: rgba(0,0,0,.55); display: flex; align-items: center; justify-content: center; z-index: 1000; padding: 20px; }
.dialog { width: 460px; max-width: 96vw; background: var(--bg-surface); border: 1px solid var(--border-subtle); border-radius: var(--radius-lg); box-shadow: var(--shadow-lg); }
.dialog-wide { width: 760px; }
.dialog-header { display: flex; align-items: center; justify-content: space-between; padding: var(--space-4); border-bottom: 1px solid var(--border-subtle); font-weight: 700; color: var(--text-primary); }
.dlg-close { border: 0; background: transparent; color: var(--text-tertiary); cursor: pointer; font-size: 18px; }
.dialog-body { padding: var(--space-4); display: flex; flex-direction: column; gap: var(--space-3); }
.dialog-footer { display: flex; justify-content: flex-end; gap: var(--space-2); padding: var(--space-4); border-top: 1px solid var(--border-subtle); }
.field-label { display: block; color: var(--text-secondary); font-size: var(--text-xs); font-weight: 700; margin-bottom: 6px; }
.field-input, .field-select { width: 100%; padding: 9px 10px; background: var(--bg-elevated); border: 1px solid var(--border-subtle); border-radius: var(--radius-md); color: var(--text-primary); }
.field-help { display: block; margin-top: 6px; color: var(--text-tertiary); font-size: var(--text-xs); }
.textarea { resize: vertical; }
.toggle-row { display: inline-flex; align-items: center; gap: 8px; color: var(--text-secondary); font-size: var(--text-sm); }
.route-form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
.form-span { grid-column: 1 / -1; }
.route-base-info { grid-column: 1 / -1; }
.base-box { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-3); padding: 10px 12px; background: var(--bg-elevated); border: 1px solid var(--border-subtle); border-radius: var(--radius-md); }
.base-box div { min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.base-box span { color: var(--text-tertiary); font-size: var(--text-xs); }
.base-box code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-secondary); font-family: var(--font-mono); font-size: var(--text-xs); }
.base-warn { color: var(--color-warning); font-size: var(--text-sm); }
.uri-field { position: relative; }
.uri-suggestions { position: absolute; left: 0; right: 0; top: 70px; z-index: 2; display: flex; flex-direction: column; max-height: 240px; overflow: auto; background: var(--bg-surface); border: 1px solid var(--border-subtle); border-radius: var(--radius-md); box-shadow: var(--shadow-lg); }
.uri-suggestion { width: 100%; padding: 9px 10px; border: 0; border-bottom: 1px solid var(--border-subtle); background: transparent; color: var(--text-secondary); text-align: left; font-family: var(--font-mono); font-size: var(--text-xs); cursor: pointer; }
.uri-suggestion:hover { background: var(--bg-elevated); color: var(--text-primary); }
.uri-suggestion:last-child { border-bottom: 0; }
.req { color: var(--color-danger); }
.fade-enter-active, .fade-leave-active { transition: opacity .18s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
@media (max-width: 900px) {
  .page-header, .section-head { flex-direction: column; }
  .stats-row { grid-template-columns: repeat(2, minmax(120px, 1fr)); }
  .filter-bar { flex-direction: column; align-items: stretch; }
  .small-select { width: 100%; }
  .route-form-grid { grid-template-columns: 1fr; }
  .base-box { grid-template-columns: 1fr; }
}
</style>
