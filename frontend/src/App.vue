<script setup lang="ts">
import { computed, nextTick, onMounted, onBeforeUnmount, ref } from 'vue'
import {
  PanelsTopLeft,
  Layers3,
  BookOpen,
  FolderOpen,
  RefreshCw,
  Search,
  ArrowRight,
  PackageCheck,
  X,
  ExternalLink,
  Folder,
  KeyRound,
  LogOut,
  Plus,
  Trash2,
  Download,
  Upload,
  AlertTriangle,
  Settings2,
  SlidersHorizontal,
  CloudDownload,
  Pencil,
} from 'lucide-vue-next'
import SkillsView from './SkillsView.vue'
import KnowledgeView from './KnowledgeView.vue'
import EnvironmentsView from './EnvironmentsView.vue'
import { version } from '../package.json'
import brandMark from '../../brand.svg'
import { statusTone } from './status'
import {
  api,
  type Env,
  type EnvTag,
  type Panel,
  type RemotePanelOption,
  type Report,
  type Target,
  type View,
} from './bridge'

const view = ref<View>(),
  selectedAliases = ref<string[]>([]),
  report = ref<Report>()
const error = ref(''),
  toast = ref(''),
  busy = ref(false),
  progress = ref(''),
  phase = ref<'env' | 'panel' | 'preview' | 'confirm' | 'results'>('env')
const dialog = ref<HTMLDialogElement>(),
  modal = ref(false)
const skipOnboarding = ref(false)
const activeEnvName = ref('')

// 界面化环境管理 / 登录表单状态
const editingEnvName = ref('')
const envForm = ref({
  Name: '',
  URL: '',
  Tag: '' as EnvTag,
  Account: '',
  Password: '',
})
const continueCheckAfterAuth = ref(false)

// 业务面板与目标资源键配置状态
const editingPanelAlias = ref('')
const panelForm = ref({
  Name: '',
  Source: '',
  Binding: '',
  ApplyAllEnvs: true,
})
const remotePanels = ref<RemotePanelOption[]>([])
const remotePanelsLoading = ref(false)
const remotePanelsError = ref('')

type Page = 'panels' | 'skill' | 'knowledge' | 'environments'
const page = ref<Page>('panels'),
  lastResourcePage = ref<'panels' | 'skill' | 'knowledge'>('panels'),
  subBusy = ref(false),
  search = ref(''),
  filter = ref<'all' | 'ready' | 'missing'>('all')
const searchBox = ref<HTMLInputElement>()

const navigation = [
  { id: 'panels' as const, label: '业务面板', icon: PanelsTopLeft },
  { id: 'skill' as const, label: '企业Skill', icon: Layers3 },
  { id: 'knowledge' as const, label: '知识库文档', icon: BookOpen },
]

const currentEnv = computed<Env | undefined>(() =>
  view.value?.Environments.find((e) => e.Name === activeEnvName.value),
)
const anyLoggedIn = computed(
  () => view.value?.Environments.some((e) => e.LoggedIn) ?? false,
)
const showOnboarding = computed(
  () => Boolean(view.value) && !anyLoggedIn.value && !skipOnboarding.value,
)
const editingEnv = computed<Env | undefined>(() =>
  view.value?.Environments.find((e) => e.Name === editingEnvName.value),
)

const pending = computed(
  () => view.value?.Panels.filter((p) => !p.Source).length ?? 0,
)
const readyCount = computed(
  () => (view.value?.Panels.length ?? 0) - pending.value,
)
const boundInCurrentEnvCount = computed(() => {
  const envName = activeEnvName.value
  if (!envName) return 0
  return (view.value?.Panels ?? []).filter((p) => Boolean(p.Bindings[envName]))
    .length
})
const visibleRows = computed(() => {
  const envName = activeEnvName.value
  const q = search.value.trim().toLowerCase()
  return (view.value?.Panels ?? []).filter(
    (p) =>
      (filter.value === 'all' ||
        Boolean(p.Source) === (filter.value === 'ready')) &&
      (!q ||
        [p.Name, p.Alias, p.Source, envName ? p.Bindings[envName] || '' : '']
          .join(' ')
          .toLowerCase()
          .includes(q)),
  )
})
const selectedTargets = computed<Target[]>(() => {
  const envName = activeEnvName.value
  if (!envName) return []
  return selectedAliases.value
    .filter((alias) =>
      view.value?.Panels.some(
        (p) => p.Alias === alias && p.Source && p.Bindings[envName],
      ),
    )
    .map((Alias) => ({ Env: envName, Alias }))
})
const ready = computed(
  () => report.value?.Results.filter((r) => r.Status === '就绪').length ?? 0,
)
const published = computed(
  () => report.value?.Results.filter((r) => r.Status === '已发布').length ?? 0,
)
const title = computed(() => {
  if (phase.value === 'env') {
    if (!editingEnvName.value) return '添加环境'
    return (
      (editingEnv.value?.LoggedIn ? '管理环境 · ' : '登录环境 · ') +
      editingEnvName.value
    )
  }
  if (phase.value === 'panel') {
    return editingPanelAlias.value ? '配置面板与目标资源键' : '添加业务面板'
  }
  return {
    preview: '本地打包结果',
    confirm: '核对发布目标',
    results: '发布结果',
  }[phase.value]
})

function showToast(msg: string) {
  toast.value = msg
  window.setTimeout(() => {
    if (toast.value === msg) toast.value = ''
  }, 4000)
}
function envHost(url: string) {
  try {
    return new URL(url).host
  } catch {
    return url
  }
}
function selectActiveEnv(envName: string) {
  if (page.value === 'environments') {
    page.value = lastResourcePage.value
  }
  if (activeEnvName.value === envName) return
  activeEnvName.value = envName
  selectedAliases.value = selectedAliases.value.filter((alias) =>
    view.value?.Panels.some(
      (p) => p.Alias === alias && p.Source && p.Bindings[envName],
    ),
  )
}
async function navigate(next: Page) {
  if (next === page.value) return
  await task(async () => {
    await api().Cancel()
    if (next !== 'environments') lastResourcePage.value = next
    page.value = next
  })
}
function clearFilter() {
  search.value = ''
  filter.value = 'all'
}
function keydown(e: KeyboardEvent) {
  if (
    (e.metaKey || e.ctrlKey) &&
    e.key.toLowerCase() === 'f' &&
    page.value === 'panels' &&
    !modal.value &&
    !showOnboarding.value
  ) {
    e.preventDefault()
    searchBox.value?.focus()
    searchBox.value?.select()
  }
}
onMounted(() => window.addEventListener('keydown', keydown))
onBeforeUnmount(() => window.removeEventListener('keydown', keydown))

function panelBindable(p: Panel) {
  return Boolean(
    p.Source && activeEnvName.value && p.Bindings[activeEnvName.value],
  )
}
function picked(alias: string) {
  return selectedAliases.value.includes(alias)
}
function toggle(alias: string) {
  selectedAliases.value = picked(alias)
    ? selectedAliases.value.filter((a) => a !== alias)
    : [...selectedAliases.value, alias]
}
const availableRows = computed(() => visibleRows.value.filter(panelBindable))
const allSelected = computed(
  () =>
    availableRows.value.length > 0 &&
    availableRows.value.every((p) => picked(p.Alias)),
)
const someSelected = computed(
  () =>
    !allSelected.value && availableRows.value.some((p) => picked(p.Alias)),
)
function toggleSelectAll() {
  const add = !allSelected.value
  const visible = new Set(availableRows.value.map((p) => p.Alias))
  selectedAliases.value = selectedAliases.value.filter((a) => !visible.has(a))
  if (add) {
    selectedAliases.value.push(...availableRows.value.map((p) => p.Alias))
  }
}

async function task(action: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  progress.value = ''
  try {
    await action()
  } catch (e) {
    error.value = String(e)
  } finally {
    busy.value = false
    progress.value = ''
  }
}
async function load() {
  const next = await api().Load()
  if (view.value && view.value.Stamp !== next.Stamp) {
    await api().Cancel()
    selectedAliases.value = []
  }
  view.value = next
  if (
    !activeEnvName.value ||
    !next.Environments.some((e) => e.Name === activeEnvName.value)
  ) {
    activeEnvName.value =
      next.Environments.find((e) => e.LoggedIn)?.Name ??
      next.Environments[0]?.Name ??
      ''
  } else {
    const cur = next.Environments.find((e) => e.Name === activeEnvName.value)
    if (cur && !cur.LoggedIn) {
      const logged = next.Environments.find((e) => e.LoggedIn)
      if (logged) activeEnvName.value = logged.Name
    }
  }
  selectedAliases.value = selectedAliases.value.filter((alias) =>
    next.Panels.some(
      (p) => p.Alias === alias && p.Source && p.Bindings[activeEnvName.value],
    ),
  )
}
async function setup(
  action: 'root' | 'config' | 'export' | 'source' | 'reload',
  alias = '',
) {
  await task(async () => {
    if (action === 'root') await api().ChooseRoot()
    if (action === 'config') {
      await api().ChooseConfig()
      showToast('已导入配置文件')
    }
    if (action === 'export') {
      const saved = await api().ExportConfig()
      if (saved) showToast('已导出配置到 ' + saved.split(/[/\\]/).pop())
      return
    }
    if (action === 'source' && view.value)
      await api().ChooseSource(view.value.Stamp, alias)
    await load()
  })
}
async function show() {
  modal.value = true
  await nextTick()
  if (!dialog.value?.open) dialog.value?.showModal()
  await nextTick()
  if (phase.value === 'env') {
    const target = editingEnvName.value
      ? dialog.value?.querySelector<HTMLInputElement>(
          envForm.value.Account
            ? 'input[type="password"]'
            : 'input[autocomplete="username"]',
        )
      : dialog.value?.querySelector<HTMLInputElement>('input')
    if (target) {
      target.focus()
      return
    }
  }
  dialog.value?.focus()
}
async function close() {
  if (busy.value) return
  await task(async () => {
    try {
      await api().Cancel()
    } finally {
      dialog.value?.close()
      modal.value = false
      envForm.value.Password = ''
      continueCheckAfterAuth.value = false
      report.value = undefined
    }
  })
}
async function openAddEnv() {
  if (busy.value || subBusy.value || !view.value) return
  error.value = ''
  phase.value = 'env'
  report.value = undefined
  editingEnvName.value = ''
  envForm.value = { Name: '', URL: '', Tag: '', Account: '', Password: '' }
  continueCheckAfterAuth.value = false
  await show()
}
async function openEnvAuth(envName: string, autoCheck = false) {
  if (busy.value || subBusy.value || !view.value) return
  const target = view.value.Environments.find((e) => e.Name === envName)
  if (!target) return
  error.value = ''
  phase.value = 'env'
  report.value = undefined
  editingEnvName.value = envName
  envForm.value = {
    Name: target.Name,
    URL: target.URL,
    Tag: target.Tag || '',
    Account: target.Account || '',
    Password: '',
  }
  continueCheckAfterAuth.value = autoCheck
  await show()
}
async function submitEnvForm() {
  if (!view.value) return
  const prevName = editingEnvName.value
  const shouldContinue = continueCheckAfterAuth.value
  const form = envForm.value
  const hasPassword = Boolean(form.Password)
  const existing = editingEnv.value
  const metadataChanged =
    !prevName ||
    form.Name.trim() !== prevName ||
    form.URL.trim().replace(/\/+$/, '') !==
      (existing?.URL || '').replace(/\/+$/, '') ||
    form.Tag !== (existing?.Tag || '')

  let ok = false
  await task(async () => {
    try {
      if (metadataChanged) {
        await api().SaveEnvironment(view.value!.Stamp, prevName, {
          Name: form.Name.trim(),
          URL: form.URL.trim(),
          Tag: form.Tag,
          Account: form.Account.trim(),
          Password: form.Password,
          Login:
            hasPassword ||
            (!existing?.LoggedIn && Boolean(form.Account.trim())),
        })
      } else if (hasPassword) {
        await api().Login(view.value!.Stamp, prevName, {
          Name: form.Account.trim(),
          Password: form.Password,
        })
      }
      await load()
      if (form.Name.trim()) activeEnvName.value = form.Name.trim()
      ok = true
    } finally {
      envForm.value.Password = ''
    }
  })
  if (!ok) return
  if (shouldContinue) {
    continueCheckAfterAuth.value = false
    await runCheck()
    return
  }
  dialog.value?.close()
  modal.value = false
}
async function logoutEnvByName(envName: string) {
  if (!view.value || !envName) return
  let ok = false
  await task(async () => {
    await api().Logout(view.value!.Stamp, envName)
    await load()
    ok = true
  })
  if (ok) {
    if (modal.value) {
      dialog.value?.close()
      modal.value = false
    }
    envForm.value.Password = ''
    continueCheckAfterAuth.value = false
    showToast(`已退出环境 ${envName}`)
  }
}
async function deleteEnvByName(envName: string) {
  if (!view.value || !envName) return
  let ok = false
  await task(async () => {
    await api().DeleteEnvironment(view.value!.Stamp, envName)
    await load()
    ok = true
  })
  if (ok) {
    if (modal.value) {
      dialog.value?.close()
      modal.value = false
    }
    showToast(`已移除环境 ${envName}`)
  }
}
async function moveEnv(envName: string, delta: -1 | 1) {
  if (!view.value) return
  const list = view.value.Environments.map((e) => e.Name)
  const idx = list.indexOf(envName)
  const target = idx + delta
  if (idx < 0 || target < 0 || target >= list.length) return
  const next = [...list]
  ;[next[idx], next[target]] = [next[target], next[idx]]
  await task(async () => {
    await api().ReorderEnvironments(view.value!.Stamp, next)
    await load()
  })
}
async function loadRemotePanelOptions() {
  remotePanels.value = []
  remotePanelsError.value = ''
  if (!currentEnv.value?.LoggedIn) return
  remotePanelsLoading.value = true
  try {
    remotePanels.value = await api().ListRemotePanels(currentEnv.value.Name)
  } catch (e) {
    remotePanelsError.value = String(e)
  } finally {
    remotePanelsLoading.value = false
  }
}
async function openAddPanel() {
  if (busy.value || subBusy.value || !view.value) return
  error.value = ''
  phase.value = 'panel'
  report.value = undefined
  editingPanelAlias.value = ''
  panelForm.value = { Name: '', Source: '', Binding: '', ApplyAllEnvs: true }
  void loadRemotePanelOptions()
  await show()
}
async function openEditPanel(p: Panel) {
  if (busy.value || subBusy.value || !view.value) return
  error.value = ''
  phase.value = 'panel'
  report.value = undefined
  editingPanelAlias.value = p.Alias
  panelForm.value = {
    Name: p.Name,
    Source: p.Source,
    Binding: activeEnvName.value ? p.Bindings[activeEnvName.value] || '' : '',
    ApplyAllEnvs: false,
  }
  void loadRemotePanelOptions()
  await show()
}
function pickRemotePanel(binding: string) {
  if (!binding) return
  panelForm.value.Binding = binding
  const matched = remotePanels.value.find((r) => r.Binding === binding)
  if (matched && !panelForm.value.Name.trim()) {
    panelForm.value.Name = matched.DisplayName
  }
}
async function submitPanelForm() {
  if (!view.value) return
  let ok = false
  await task(async () => {
    await api().SavePanel(
      view.value!.Stamp,
      editingPanelAlias.value,
      activeEnvName.value,
      {
        Alias: editingPanelAlias.value,
        Name: panelForm.value.Name.trim(),
        Source: panelForm.value.Source.trim(),
        Binding: panelForm.value.Binding.trim(),
        ApplyAllEnvs: panelForm.value.ApplyAllEnvs,
      },
    )
    await load()
    ok = true
  })
  if (ok) {
    dialog.value?.close()
    modal.value = false
    showToast(editingPanelAlias.value ? '已保存面板配置' : '已添加业务面板')
  }
}
async function deleteCurrentPanel() {
  if (!view.value || !editingPanelAlias.value) return
  const alias = editingPanelAlias.value
  let ok = false
  await task(async () => {
    await api().DeletePanel(view.value!.Stamp, alias)
    await load()
    ok = true
  })
  if (ok) {
    dialog.value?.close()
    modal.value = false
    showToast('已删除业务面板')
  }
}
async function syncRemotePanels() {
  if (!view.value || !currentEnv.value) return
  if (!currentEnv.value.LoggedIn) {
    await openEnvAuth(currentEnv.value.Name)
    return
  }
  const envName = currentEnv.value.Name
  let added = 0
  let ok = false
  await task(async () => {
    added = await api().SyncRemotePanels(view.value!.Stamp, envName)
    await load()
    ok = true
  })
  if (ok) {
    showToast(
      added > 0
        ? `已从 ${envName} 同步 ${added} 个远端面板`
        : `${envName} 的远端面板已全部在列表中`,
    )
  }
}
async function preview() {
  if (!selectedTargets.value.length) return
  await task(async () => {
    phase.value = 'preview'
    report.value = undefined
    await show()
    report.value = await api().Preview(view.value!.Stamp, selectedTargets.value)
  })
}
async function startCheck() {
  if (!currentEnv.value || !selectedTargets.value.length) return
  if (!currentEnv.value.LoggedIn) {
    await openEnvAuth(currentEnv.value.Name, true)
    return
  }
  await runCheck()
}
async function runCheck() {
  await task(async () => {
    phase.value = 'confirm'
    report.value = undefined
    await show()
    report.value = await api().Check(
      view.value!.Stamp,
      selectedTargets.value,
      {},
    )
  })
}
async function publish() {
  await task(async () => {
    const id = report.value?.ID
    if (!id) return
    report.value!.ID = ''
    report.value = await api().Publish(id)
    phase.value = 'results'
  })
}
function size(n: number) {
  return n >= 1048576
    ? (n / 1048576).toFixed(1) + ' MiB'
    : Math.ceil(n / 1024) + ' KiB'
}
function openResult(url: string) {
  window.runtime.BrowserOpenURL(url)
}
onMounted(() => {
  window.runtime?.EventsOn('panel-progress', (value) => {
    progress.value = value
  })
  void setup('reload')
})
</script>
<template>
  <div class="shell" :class="{ 'is-onboarding': showOnboarding }">
    <!-- 首次启动前置登录引导视图 -->
    <main v-if="showOnboarding && view" class="onboarding">
      <div class="onboarding-card">
        <div class="onboarding-head">
          <img class="onboarding-logo" :src="brandMark" alt="" />
          <div>
            <div class="brand-name">
              <strong>HiveBridge</strong>
              <span class="version-tag">{{ version }}</span>
            </div>
            <p>
              先登录一个蜂巢环境，凭据将加密保存在本机，后续直接管理业务面板、企业Skill与知识库。
            </p>
          </div>
        </div>

        <div v-if="view.Environments.length" class="onboarding-envs">
          <div class="nav-label">选择已有环境登录</div>
          <div class="onboarding-list">
            <button
              v-for="e in view.Environments"
              :key="e.Name"
              class="onboarding-env-row"
              :disabled="busy"
              :aria-label="'登录环境 ' + e.Name"
              @click="openEnvAuth(e.Name)"
            >
              <div class="onboarding-env-info">
                <div class="onboarding-env-title">
                  <strong>{{ e.Name }}</strong>
                </div>
                <code>{{ e.URL }}</code>
              </div>
              <span class="onboarding-login-cta">
                <KeyRound :size="13" :stroke-width="2" />登录
              </span>
            </button>
          </div>
        </div>

        <div class="onboarding-actions">
          <button class="primary" :disabled="busy" @click="openAddEnv">
            <Plus :size="14" :stroke-width="2" />添加新环境
          </button>
          <button :disabled="busy" @click="setup('config')">
            <Upload :size="13.5" :stroke-width="2" />导入配置
          </button>
          <button
            class="ghost-btn onboarding-skip"
            :disabled="busy"
            @click="skipOnboarding = true"
          >
            暂不登录，仅离线预览面板
          </button>
        </div>
        <p v-if="error && !modal" class="error" role="alert">{{ error }}</p>
      </div>
    </main>

    <!-- 正常桌面主工作区 -->
    <template v-else>
      <aside class="sidebar">
        <div class="brand">
          <img class="appmark" :src="brandMark" alt="" />
          <div class="brand-copy">
            <div class="brand-name">
              <strong>HiveBridge</strong>
            </div>
            <small>HiveOS 开发者工具</small>
          </div>
        </div>

        <div class="nav-group">
          <div class="nav-label">资源</div>
          <nav aria-label="发布资源">
            <button
              v-for="n in navigation"
              :key="n.id"
              :aria-current="page === n.id ? 'page' : undefined"
              :class="{ active: page === n.id }"
              :disabled="busy || subBusy"
              @click="navigate(n.id)"
            >
              <component :is="n.icon" :size="15" :stroke-width="1.9" />
              <span>{{ n.label }}</span>
              <span v-if="n.id === 'panels' && view" class="nav-badge">{{
                view.Panels.length
              }}</span>
            </button>
          </nav>
        </div>

        <!-- 左侧独立可滚动环境切换区（适配 4~10+ 套环境） -->
        <section class="side-env-panel" aria-label="环境列表">
          <div class="nav-label-row">
            <span class="nav-label">
              环境
              <small v-if="view" class="env-total">{{
                view.Environments.length
              }}</small>
            </span>
            <div class="side-env-tools">
              <button
                class="side-add-btn"
                :class="{ 'is-active-tool': page === 'environments' }"
                :disabled="busy || subBusy"
                aria-label="环境管理"
                title="管理全部环境"
                @click="navigate('environments')"
              >
                <Settings2 :size="13" :stroke-width="2" />
              </button>
              <button
                class="side-add-btn"
                :disabled="busy || subBusy"
                aria-label="添加环境"
                title="添加环境"
                @click="openAddEnv"
              >
                <Plus :size="13.5" :stroke-width="2.2" />
              </button>
            </div>
          </div>

          <div class="side-env-scroll">
            <div
              v-for="e in view?.Environments"
              :key="e.Name"
              class="side-env-item"
              :class="{
                active: e.Name === activeEnvName && page !== 'environments',
                'is-prod': e.Tag === 'prod',
              }"
              :title="
                e.LoggedIn
                  ? e.Name + '（已登录 ' + e.Account + '）· ' + e.URL
                  : e.Name + '（未登录）· ' + e.URL
              "
            >
              <button
                class="side-env-select"
                :aria-pressed="
                  e.Name === activeEnvName && page !== 'environments'
                "
                :aria-label="'切换到环境 ' + e.Name"
                :disabled="busy || subBusy"
                @click="selectActiveEnv(e.Name)"
              >
                <span class="env-dot" :class="{ connected: e.LoggedIn }" />
                <span class="env-copy">
                  <span class="env-name-row">
                    <span class="env-name">{{ e.Name }}</span>
                  </span>
                  <small class="env-sub">{{ envHost(e.URL) }}</small>
                </span>
              </button>
              <button
                v-if="!e.LoggedIn"
                class="side-env-login"
                :disabled="busy || subBusy"
                :aria-label="'登录环境 ' + e.Name"
                @click.stop="openEnvAuth(e.Name)"
              >
                登录
              </button>
              <button
                v-else
                class="side-env-manage"
                :disabled="busy || subBusy"
                :aria-label="'管理环境 ' + e.Name"
                title="账号与环境设置"
                @click.stop="openEnvAuth(e.Name)"
              >
                <SlidersHorizontal :size="12" :stroke-width="2" />
              </button>
            </div>
            <div v-if="view && !view.Environments.length" class="side-env-empty">
              暂无环境，点击右上角 + 添加
            </div>
          </div>
        </section>

        <div class="side-bottom">
          <div class="side-config-actions">
            <button
              class="config-half-btn"
              :disabled="busy || subBusy"
              title="从 JSON 文件导入环境与面板配置"
              @click="setup('config')"
            >
              <Upload :size="13" :stroke-width="2" />导入配置
            </button>
            <button
              class="config-half-btn"
              :disabled="busy || subBusy"
              title="将当前配置导出为 JSON 文件分享给团队"
              @click="setup('export')"
            >
              <Download :size="13" :stroke-width="2" />导出配置
            </button>
          </div>
        </div>
      </aside>

      <div class="workspace">
        <SkillsView
          v-if="page === 'skill' && view"
          :view="view"
          :env="activeEnvName"
          @busy="subBusy = $event"
          @login="openEnvAuth($event)"
        />

        <KnowledgeView
          v-if="page === 'knowledge' && view"
          :view="view"
          :env="activeEnvName"
          @busy="subBusy = $event"
          @login="openEnvAuth($event)"
        />

        <EnvironmentsView
          v-if="page === 'environments' && view"
          :view="view"
          :active-env="activeEnvName"
          :busy="busy"
          @select-env="selectActiveEnv($event)"
          @add="openAddEnv"
          @edit="openEnvAuth($event)"
          @logout="logoutEnvByName($event)"
          @delete="deleteEnvByName($event)"
          @move="moveEnv"
        />

        <template v-if="page === 'panels'">
          <header class="win-toolbar">
            <div class="win-title">
              <h1>业务面板</h1>
              <span
                v-if="currentEnv"
                class="toolbar-env-badge"
              >
                <span
                  class="env-dot"
                  :class="{ connected: currentEnv.LoggedIn }"
                />
                <span>{{ currentEnv.Name }}</span>
              </span>
              <span v-if="view" class="title-count"
                >{{ view.Panels.length }} 项</span
              >
            </div>
            <div class="win-controls">
              <div v-if="view" class="segmented" aria-label="筛选面板">
                <button
                  v-for="f in [
                    ['all', '全部', view.Panels.length],
                    ['ready', '已配目录', readyCount],
                    ['missing', '待配目录', pending],
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
                  ref="searchBox"
                  v-model="search"
                  type="search"
                  placeholder="搜索面板或资源键"
                  aria-label="搜索面板或资源键"
                />
                <kbd v-if="!search">⌘F</kbd>
              </label>
              <button
                class="compact-btn"
                :disabled="busy || subBusy || !currentEnv?.LoggedIn"
                title="从当前已登录环境自动拉取远端业务面板及目标资源键"
                @click="syncRemotePanels"
              >
                <CloudDownload :size="13" :stroke-width="2" />同步远端
              </button>
              <button
                class="compact-btn"
                :disabled="busy || subBusy || !view"
                title="手工添加业务面板并配置目标资源键"
                @click="openAddPanel"
              >
                <Plus :size="13" :stroke-width="2.1" />添加面板
              </button>
              <button
                class="tool-icon-btn"
                :disabled="busy || subBusy"
                @click="setup('reload')"
                aria-label="刷新配置"
                title="刷新配置"
              >
                <RefreshCw :size="14" :stroke-width="1.9" />
              </button>
            </div>
          </header>

          <div class="pathbar">
            <div class="pathbar-main">
              <FolderOpen :size="14" :stroke-width="1.9" />
              <span class="pathbar-label">默认源码根目录</span>
              <span class="pathbar-sep">/</span>
              <code
                class="pathbar-value"
                :class="{ placeholder: !view?.Root }"
                :title="view?.Root"
                >{{
                  view?.Root || '可选，未设置时可直接为单个面板选择任意本机目录'
                }}</code
              >
            </div>
            <button class="compact-btn" :disabled="busy" @click="setup('root')">
              {{ view?.Root ? '更换目录' : '选择文件夹' }}
            </button>
          </div>

          <div v-if="currentEnv && !currentEnv.LoggedIn" class="auth-banner">
            <div class="auth-banner-copy">
              <KeyRound :size="13.5" :stroke-width="2" />
              <span
                >当前环境 <strong>{{ currentEnv.Name }}</strong> 尚未登录，可先本地打包预览，或登录后检查并发布。</span
              >
            </div>
            <button
              class="compact-btn"
              :disabled="busy"
              @click="openEnvAuth(currentEnv.Name)"
            >
              立即登录
            </button>
          </div>

          <div
            v-else-if="currentEnv?.LoggedIn && boundInCurrentEnvCount === 0"
            class="auth-banner"
          >
            <div class="auth-banner-copy">
              <CloudDownload :size="13.5" :stroke-width="2" />
              <span
                >当前环境 <strong>{{ currentEnv.Name }}</strong> 尚未绑定业务面板，可一键从远端拉取并自动填入目标资源键。</span
              >
            </div>
            <button
              class="compact-btn"
              :disabled="busy"
              @click="syncRemotePanels"
            >
              一键同步远端面板
            </button>
          </div>

          <p v-if="toast" class="toast-strip" role="status">
            {{ toast }}
          </p>
          <p v-if="error && !modal" class="error strip-error" role="alert">
            {{ error }}
          </p>

          <main class="matrix-pane">
            <table v-if="view" class="panel-list-table">
              <thead>
                <tr>
                  <th scope="col" class="col-check-cell">
                    <input
                      type="checkbox"
                      aria-label="全选当前环境面板"
                      :checked="allSelected"
                      :indeterminate="someSelected"
                      :disabled="busy || !availableRows.length"
                      @change="toggleSelectAll"
                    />
                  </th>
                  <th scope="col">面板 / 本地上传目录</th>
                  <th scope="col">目标资源键</th>
                  <th scope="col" class="col-status-cell">状态</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="p in visibleRows"
                  :key="p.Alias"
                  :class="{
                    'row-chosen': picked(p.Alias),
                    'row-disabled': !panelBindable(p),
                  }"
                >
                  <td class="col-check-cell">
                    <input
                      type="checkbox"
                      :aria-label="'选择面板 ' + p.Name"
                      :disabled="!panelBindable(p) || busy"
                      :checked="picked(p.Alias)"
                      @change="toggle(p.Alias)"
                    />
                  </td>
                  <th scope="row" class="panel-name-col">
                    <div class="row-meta">
                      <div class="row-name-line">
                        <strong>{{ p.Name }}</strong>
                        <span
                          v-if="!p.Source"
                          class="row-pending-dot"
                          title="待配置上传目录"
                        />
                      </div>
                      <button
                        class="source single-line"
                        :class="{ missing: !p.Source }"
                        :disabled="busy"
                        :title="
                          p.Source
                            ? p.Source + '（点击更换）'
                            : '选择并验证本地上传目录（支持任意本机目录）'
                        "
                        @click="setup('source', p.Alias)"
                      >
                        <Folder :size="11.5" :stroke-width="2" />
                        <span>{{ p.Source || '选择上传目录' }}</span>
                      </button>
                    </div>
                  </th>
                  <td class="panel-binding-col">
                    <button
                      class="binding-edit-btn"
                      :disabled="busy"
                      :aria-label="'配置面板 ' + p.Name + ' 的目标资源键'"
                      title="点击配置面板名称与当前环境目标资源键"
                      @click="openEditPanel(p)"
                    >
                      <code
                        v-if="activeEnvName && p.Bindings[activeEnvName]"
                        class="binding-code"
                        >{{ p.Bindings[activeEnvName] }}</code
                      >
                      <span v-else class="unset-inline">— 点击配置资源键</span>
                      <Pencil
                        class="binding-edit-icon"
                        :size="11.5"
                        :stroke-width="2"
                      />
                    </button>
                  </td>
                  <td class="col-status-cell">
                    <span
                      v-if="panelBindable(p)"
                      class="status"
                      data-tone="success"
                      >可发布</span
                    >
                    <span
                      v-else-if="!p.Source"
                      class="status"
                      data-tone="warning"
                      >待配目录</span
                    >
                    <span v-else class="status" data-tone="neutral">未绑定</span>
                  </td>
                </tr>
                <tr v-if="!visibleRows.length">
                  <td colspan="4" class="empty">
                    {{
                      view.Panels.length
                        ? '没有匹配的面板。'
                        : '当前配置暂未登记业务面板，可通过“导入配置”载入面板清单。'
                    }}
                    <button
                      v-if="view.Panels.length"
                      class="compact-btn"
                      @click="clearFilter"
                    >
                      清除筛选
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </main>

          <footer class="win-footer">
            <div class="footer-status">
              <span
                class="status-dot"
                :class="{ active: selectedTargets.length > 0 }"
              />
              <span>
                已选
                <strong :class="{ countActive: selectedTargets.length }">{{
                  selectedTargets.length
                }}</strong>
                个面板
              </span>
              <span v-if="currentEnv" class="muted">
                · 目标环境 {{ currentEnv.Name }}
              </span>
            </div>
            <div class="actions">
              <button
                v-if="selectedAliases.length"
                class="ghost-btn"
                :disabled="busy"
                @click="selectedAliases = []"
              >
                清空
              </button>
              <button
                :disabled="busy || !selectedTargets.length"
                @click="preview"
              >
                <PackageCheck :size="14" :stroke-width="1.9" />本地打包预览
              </button>
              <button
                class="primary"
                :disabled="busy || !selectedTargets.length"
                @click="startCheck"
              >
                检查发布目标 <ArrowRight :size="14" :stroke-width="2" />
              </button>
            </div>
          </footer>
        </template>
      </div>
    </template>

    <dialog
      ref="dialog"
      tabindex="-1"
      aria-labelledby="modal-title"
      @cancel.prevent="close"
    >
      <div class="dialog-head">
        <h2 id="modal-title">{{ title }}</h2>
        <button class="close" aria-label="关闭" :disabled="busy" @click="close">
          <X :size="15" :stroke-width="2" />
        </button>
      </div>

      <!-- 添加 / 编辑 / 登录环境 -->
      <form v-if="modal && phase === 'env'" @submit.prevent="submitEnvForm">
        <div class="body">
          <div v-if="editingEnv" class="auth-env-bar">
            <div class="auth-env-id">
              <span class="env-pill">{{ editingEnv.Name }}</span>
              <code>{{ editingEnv.URL }}</code>
            </div>
            <b
              class="status"
              :data-tone="editingEnv.LoggedIn ? 'success' : 'neutral'"
            >
              {{
                editingEnv.LoggedIn
                  ? '已登录 · ' + editingEnv.Account
                  : '未登录'
              }}
            </b>
          </div>

          <p class="hint auth-hint">
            {{
              continueCheckAfterAuth
                ? '当前环境尚未登录，登录后将加密保存在本机并自动继续检查。'
                : !editingEnvName
                  ? '填写环境地址与管理员账号，校验通过后自动加密记住，下次启动无需重复登录。'
                  : editingEnv?.LoggedIn
                    ? '可修改环境名称、地址、类型标签，或在下方更新管理员密码、退出登录。'
                    : '登录验证通过后将加密保存在本机，后续重启应用也无需重复输入。'
            }}
          </p>

          <div class="inset-group auth-fields">
            <label class="stack-label">
              环境名称
              <input
                v-model="envForm.Name"
                required
                maxlength="48"
                placeholder="例如 开发环境 或 测试环境"
                :disabled="busy"
              />
            </label>

            <label class="stack-label" style="margin-top: 12px">
              服务地址
              <input
                v-model="envForm.URL"
                required
                placeholder="例如 https://hive.example.com"
                :disabled="busy"
              />
            </label>

            <div class="fields" style="margin-top: 12px">
              <label>
                管理员账号
                <input
                  v-model="envForm.Account"
                  :required="!editingEnv?.LoggedIn"
                  autocomplete="username"
                  placeholder="例如 admin"
                  :disabled="busy"
                />
              </label>
              <label>
                密码
                <input
                  v-model="envForm.Password"
                  type="password"
                  :required="!editingEnv?.LoggedIn"
                  autocomplete="off"
                  :placeholder="editingEnv?.LoggedIn ? '留空保持原密码' : ''"
                  :disabled="busy"
                />
              </label>
            </div>

            <label class="check-row" style="margin-top: 12px">
              <input
                type="checkbox"
                :checked="envForm.Tag === 'prod'"
                :disabled="busy"
                @change="
                  envForm.Tag = ($event.target as HTMLInputElement).checked
                    ? 'prod'
                    : ''
                "
              />
              <span>发布前显示强确认提示（建议用于客户现场或生产环境）</span>
            </label>
          </div>
        </div>
        <p v-if="error" class="error dialog-error" role="alert">{{ error }}</p>
        <div class="dialog-foot">
          <div v-if="editingEnv" class="foot-left-actions">
            <button
              v-if="editingEnv.LoggedIn"
              type="button"
              class="ghost-btn danger-text"
              :disabled="busy"
              @click="logoutEnvByName(editingEnv.Name)"
            >
              <LogOut :size="13" :stroke-width="2" />退出登录
            </button>
            <button
              type="button"
              class="ghost-btn danger-text"
              :disabled="busy"
              @click="deleteEnvByName(editingEnv.Name)"
            >
              <Trash2 :size="13" :stroke-width="2" />删除环境
            </button>
          </div>
          <span v-else role="status">{{ progress }}</span>
          <button type="button" :disabled="busy" @click="close">取消</button>
          <button class="primary" :disabled="busy">
            {{
              busy
                ? '正在保存…'
                : !editingEnvName
                  ? '添加并登录'
                  : editingEnv?.LoggedIn
                    ? '保存修改'
                    : '登录并记住'
            }}
          </button>
        </div>
      </form>

      <!-- 添加 / 编辑业务面板与目标资源键 -->
      <form
        v-else-if="modal && phase === 'panel'"
        @submit.prevent="submitPanelForm"
      >
        <div class="body">
          <p class="hint auth-hint">
            {{
              activeEnvName
                ? `配置面板信息与「${activeEnvName}」的目标资源键（格式：业务系统键/面板键，留空表示在当前环境解绑）。`
                : '配置面板展示名称与本地相对目录。'
            }}
          </p>

          <div class="inset-group auth-fields">
            <label v-if="currentEnv?.LoggedIn" class="stack-label">
              从「{{ currentEnv.Name }}」远端面板选择
              <select
                :disabled="busy || remotePanelsLoading"
                :value="
                  remotePanels.some((r) => r.Binding === panelForm.Binding)
                    ? panelForm.Binding
                    : ''
                "
                @change="
                  pickRemotePanel(($event.target as HTMLSelectElement).value)
                "
              >
                <option value="">
                  {{
                    remotePanelsLoading
                      ? '正在拉取远端面板列表…'
                      : remotePanels.length
                        ? '选择远端面板自动填入目标资源键…'
                        : '未找到启用的远端面板（可在下方手动输入）'
                  }}
                </option>
                <option
                  v-for="r in remotePanels"
                  :key="r.Binding"
                  :value="r.Binding"
                >
                  {{ r.DisplayName }}（{{ r.Binding
                  }}{{ r.Version ? ' · v' + r.Version : '' }}）
                </option>
              </select>
            </label>
            <p
              v-if="remotePanelsError"
              class="hint"
              style="margin-top: 6px; color: var(--error-ink)"
            >
              拉取远端面板失败：{{ remotePanelsError }}
            </p>

            <label class="stack-label" style="margin-top: 12px">
              面板名称
              <input
                v-model="panelForm.Name"
                required
                placeholder="例如 报警处置"
                :disabled="busy"
              />
            </label>

            <label
              v-if="activeEnvName"
              class="stack-label"
              style="margin-top: 12px"
            >
              {{ activeEnvName }} · 目标资源键
              <input
                v-model="panelForm.Binding"
                placeholder="例如 lighting/alarm-disposal（留空则当前环境不绑定）"
                :disabled="busy"
              />
            </label>

            <label
              v-if="
                view &&
                view.Environments.length > 1 &&
                panelForm.Binding.trim()
              "
              class="check-row"
              style="margin-top: 10px"
            >
              <input
                v-model="panelForm.ApplyAllEnvs"
                type="checkbox"
                :disabled="busy"
              />
              <span
                >同时将此目标资源键应用到全部
                {{ view.Environments.length }} 个环境</span
              >
            </label>

            <label class="stack-label" style="margin-top: 12px">
              本地目录（可选，支持相对路径或本机绝对路径）
              <input
                v-model="panelForm.Source"
                placeholder="例如 panels/alarm-disposal 或本机绝对路径（也可稍后点击选择文件夹）"
                :disabled="busy"
              />
            </label>
          </div>
        </div>
        <p v-if="error" class="error dialog-error" role="alert">{{ error }}</p>
        <div class="dialog-foot">
          <div v-if="editingPanelAlias" class="foot-left-actions">
            <button
              type="button"
              class="ghost-btn danger-text"
              :disabled="busy"
              @click="deleteCurrentPanel"
            >
              <Trash2 :size="13" :stroke-width="2" />删除面板
            </button>
          </div>
          <span v-else role="status">{{ progress }}</span>
          <button type="button" :disabled="busy" @click="close">取消</button>
          <button class="primary" :disabled="busy">
            {{ busy ? '正在保存…' : editingPanelAlias ? '保存修改' : '添加面板' }}
          </button>
        </div>
      </form>

      <!-- 预览 / 核对 / 发布结果 -->
      <template v-else>
        <div class="body">
          <div
            v-if="phase === 'confirm' && currentEnv?.Tag === 'prod'"
            class="callout warning"
          >
            <AlertTriangle :size="14" :stroke-width="2" />
            <p>
              当前目标
              <strong>{{ currentEnv.Name }}</strong>
              标记为<strong>正式</strong>环境，请仔细核对版本与绑定后再确认发布。
            </p>
          </div>
          <p v-if="phase === 'confirm' && busy && !report" class="hint">
            正在使用已记住的管理员凭据检查所选面板与版本…
          </p>
          <p v-else-if="phase === 'confirm'" class="hint">
            本次将使用检查时打包的源码，请核对环境、目录与面板标识。
          </p>
          <p v-if="phase === 'results'" class="summary">
            {{ published }} / {{ report?.Results.length }} 个目标已确认发布
          </p>
          <div v-if="report?.Results.length" class="result-group">
            <article v-for="r in report?.Results" :key="r.Env + '/' + r.Alias">
              <div class="result-title">
                <div class="result-name">
                  <strong>{{ r.Name }}</strong>
                  <span class="env-pill">{{ r.Env }}</span>
                  <code>{{ r.Binding }}</code>
                </div>
                <b class="status" :data-tone="statusTone(r.Status)">{{
                  r.Status
                }}</b>
              </div>
              <p class="result-meta muted">
                <span>{{ r.Source }}</span>
                <span v-if="r.Bytes">· {{ size(r.Bytes) }}</span>
                <span v-if="r.Version">· 检查时 v{{ r.Version }}</span>
              </p>
              <p v-if="r.Detail" class="result-detail">{{ r.Detail }}</p>
              <button
                v-if="r.URL"
                class="compact-btn open-btn"
                @click="openResult(r.URL)"
              >
                打开面板 <ExternalLink :size="12" />
              </button>
            </article>
          </div>
          <p v-if="report?.Warning" class="notice">{{ report.Warning }}</p>
          <p v-if="error" class="error" role="alert">{{ error }}</p>
        </div>
        <div class="dialog-foot">
          <span role="status">{{ progress }}</span>
          <button :disabled="busy" @click="close">
            {{ phase === 'confirm' ? '返回' : '关闭' }}
          </button>
          <button
            v-if="phase === 'confirm' && ready"
            class="primary"
            :disabled="busy || !report?.ID"
            @click="publish"
          >
            {{ busy ? '正在发布…' : '确认发布 ' + ready + ' 个就绪目标' }}
          </button>
        </div>
      </template>
    </dialog>
  </div>
</template>
