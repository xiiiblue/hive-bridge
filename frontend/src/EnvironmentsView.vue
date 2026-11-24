<script setup lang="ts">
import { computed, ref } from 'vue'
import {
  Search,
  Plus,
  KeyRound,
  LogOut,
  Pencil,
  Trash2,
  Check,
  ArrowUpRight,
  Server,
  ChevronUp,
  ChevronDown,
} from 'lucide-vue-next'
import { type Env, type View } from './bridge'

const props = defineProps<{
  view: View
  activeEnv: string
  busy?: boolean
}>()
const emit = defineEmits<{
  'select-env': [env: string]
  add: []
  edit: [env: string]
  logout: [env: string]
  delete: [env: string]
  move: [env: string, delta: -1 | 1]
}>()

const search = ref('')
const filter = ref<'all' | 'logged' | 'unlogged'>('all')
const canReorder = computed(
  () => filter.value === 'all' && search.value.trim() === '',
)

const counts = computed(() => {
  const list = props.view.Environments
  const logged = list.filter((e) => e.LoggedIn).length
  return {
    all: list.length,
    logged,
    unlogged: list.length - logged,
  }
})

const filteredEnvs = computed(() => {
  const q = search.value.trim().toLowerCase()
  return props.view.Environments.filter((e) => {
    if (filter.value === 'logged' && !e.LoggedIn) return false
    if (filter.value === 'unlogged' && e.LoggedIn) return false
    if (!q) return true
    return [e.Name, e.URL, e.Account]
      .join(' ')
      .toLowerCase()
      .includes(q)
  })
})

function bindingCount(envName: string) {
  return props.view.Panels.filter((p) => Boolean(p.Bindings[envName])).length
}

function openAdmin(e: Env) {
  window.runtime?.BrowserOpenURL(e.URL.replace(/\/$/, '') + '/admin/')
}
</script>
<template>
  <div class="manager-view">
    <header class="win-toolbar">
      <div class="win-title">
        <h1>环境管理</h1>
        <span class="title-count">
          共 {{ counts.all }} 个 · 已登录 {{ counts.logged }} 个
        </span>
      </div>
      <div class="win-controls">
        <div class="segmented" aria-label="筛选环境">
          <button
            v-for="f in [
              ['all', '全部', counts.all],
              ['logged', '已登录', counts.logged],
              ['unlogged', '未登录', counts.unlogged],
            ] as const"
            :key="f[0]"
            :aria-pressed="filter === f[0]"
            @click="filter = f[0]"
          >
            {{ f[1] }} <small>{{ f[2] }}</small>
          </button>
        </div>
        <label class="search">
          <Search :size="13" :stroke-width="2" />
          <input
            v-model="search"
            type="search"
            placeholder="搜索环境名或地址"
            aria-label="搜索环境名或地址"
          />
        </label>
        <button class="primary" :disabled="busy" @click="emit('add')">
          <Plus :size="14" :stroke-width="2" />添加环境
        </button>
      </div>
    </header>

    <main class="docs-scroll">
      <div v-if="!view.Environments.length" class="center-empty">
        <Server :size="28" :stroke-width="1.6" />
        <strong>尚未配置蜂巢环境</strong>
        <p>添加环境地址与管理员账号，登录后凭据会加密保存在本机。</p>
        <button class="primary" :disabled="busy" @click="emit('add')">
          <Plus :size="14" :stroke-width="2" />添加第一个环境
        </button>
      </div>

      <table v-else class="docs-table env-table">
        <thead>
          <tr>
            <th scope="col" class="col-order">排序</th>
            <th scope="col">环境名称</th>
            <th scope="col">服务地址</th>
            <th scope="col">登录状态</th>
            <th scope="col">已配面板</th>
            <th scope="col" class="col-actions">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(e, idx) in filteredEnvs"
            :key="e.Name"
            :class="{ 'is-current-row': e.Name === activeEnv }"
          >
            <td class="env-order-cell">
              <div class="env-order-controls">
                <span class="env-order-num">{{ idx + 1 }}</span>
                <button
                  class="tool-icon-btn mini"
                  :disabled="busy || !canReorder || idx === 0"
                  :aria-label="'上移环境 ' + e.Name"
                  :title="canReorder ? '上移' : '清空筛选后可调整排序'"
                  @click="emit('move', e.Name, -1)"
                >
                  <ChevronUp :size="13" :stroke-width="2" />
                </button>
                <button
                  class="tool-icon-btn mini"
                  :disabled="
                    busy || !canReorder || idx === filteredEnvs.length - 1
                  "
                  :aria-label="'下移环境 ' + e.Name"
                  :title="canReorder ? '下移' : '清空筛选后可调整排序'"
                  @click="emit('move', e.Name, 1)"
                >
                  <ChevronDown :size="13" :stroke-width="2" />
                </button>
              </div>
            </td>
            <td>
              <div class="env-table-name">
                <span class="env-dot" :class="{ connected: e.LoggedIn }" />
                <strong>{{ e.Name }}</strong>
                <span v-if="e.Name === activeEnv" class="current-badge">
                  当前
                </span>
              </div>
            </td>
            <td>
              <code class="env-table-url">{{ e.URL }}</code>
            </td>
            <td>
              <span
                class="status"
                :data-tone="e.LoggedIn ? 'success' : 'neutral'"
              >
                {{ e.LoggedIn ? '已登录 · ' + e.Account : '未登录' }}
              </span>
            </td>
            <td class="muted">{{ bindingCount(e.Name) }} 个</td>
            <td class="col-actions">
              <div class="row-actions">
                <button
                  v-if="e.Name !== activeEnv"
                  class="compact-btn"
                  :disabled="busy"
                  :aria-label="'切换到环境 ' + e.Name"
                  @click="emit('select-env', e.Name)"
                >
                  <Check :size="12" :stroke-width="2.2" />设为当前
                </button>
                <button
                  class="compact-btn"
                  :class="{ primary: !e.LoggedIn }"
                  :disabled="busy"
                  :aria-label="
                    e.LoggedIn ? '管理环境 ' + e.Name : '登录环境 ' + e.Name
                  "
                  @click="emit('edit', e.Name)"
                >
                  <component
                    :is="e.LoggedIn ? Pencil : KeyRound"
                    :size="12"
                    :stroke-width="2"
                  />
                  {{ e.LoggedIn ? '编辑' : '登录' }}
                </button>
                <button
                  v-if="e.LoggedIn"
                  class="tool-icon-btn mini"
                  :disabled="busy"
                  :aria-label="'退出环境 ' + e.Name"
                  title="退出登录"
                  @click="emit('logout', e.Name)"
                >
                  <LogOut :size="12.5" :stroke-width="2" />
                </button>
                <button
                  class="tool-icon-btn mini"
                  title="在浏览器打开管理台"
                  aria-label="在浏览器打开管理台"
                  @click="openAdmin(e)"
                >
                  <ArrowUpRight :size="13" :stroke-width="2" />
                </button>
                <button
                  class="tool-icon-btn mini danger-text"
                  :disabled="busy"
                  :aria-label="'删除环境 ' + e.Name"
                  title="删除环境"
                  @click="emit('delete', e.Name)"
                >
                  <Trash2 :size="12.5" :stroke-width="2" />
                </button>
              </div>
            </td>
          </tr>
          <tr v-if="!filteredEnvs.length">
            <td colspan="6" class="empty">
              没有匹配的环境。
              <button
                class="compact-btn"
                @click="(search = ''), (filter = 'all')"
              >
                清除筛选
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </main>
  </div>
</template>
