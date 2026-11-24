<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import {
  Search,
  RefreshCw,
  Upload,
  FolderUp,
  Download,
  Rocket,
  Pencil,
  Power,
  ExternalLink,
  KeyRound,
  X,
  CheckCircle2,
  FileUp,
  Info,
  AlertTriangle,
  Layers3,
} from 'lucide-vue-next'
import {
  api,
  fileToBase64,
  type SkillCatalog,
  type SkillDetail,
  type SkillDirEntry,
  type SkillItem,
  type UploadReceipt,
  type UploadReview,
  type View,
} from './bridge'
import { statusTone } from './status'

const props = defineProps<{
  view: View
  env: string
}>()
const emit = defineEmits<{
  busy: [value: boolean]
  login: [env: string]
}>()

const catalog = ref<SkillCatalog>()
const selectedId = ref('')
const detail = ref<SkillDetail>()
const loading = ref(false)
const detailLoading = ref(false)
const busy = ref(false)
const error = ref('')
const toast = ref('')
const search = ref('')
const filter = ref<'active' | 'draft' | 'disabled' | 'all'>('active')
const dragging = ref(false)

// 弹窗状态：upload | edit
const dialog = ref<HTMLDialogElement>()
const modalMode = ref<'upload' | 'edit' | ''>('')
const review = ref<UploadReview>()
const receipt = ref<UploadReceipt>()
const uploadScope = ref('')
const uploadDisplay = ref('')
const uploadExample = ref('')
const uploadPublish = ref(false)

const editDisplay = ref('')
const editExample = ref('')
const editScope = ref('*')

const currentEnv = computed(() =>
  props.view.Environments.find((e) => e.Name === props.env),
)
const isProd = computed(() => currentEnv.value?.Tag === 'prod')

const filteredSkills = computed(() => {
  const list = catalog.value?.Skills ?? []
  const q = search.value.trim().toLowerCase()
  return list.filter((s) => {
    if (filter.value === 'active' && s.Status !== 'ACTIVE') return false
    if (filter.value === 'draft' && !s.HasDraft) return false
    if (filter.value === 'disabled' && s.Status !== 'DISABLED') return false
    if (!q) return true
    return [s.Display, s.Key, s.Description, ...s.ScopeNames]
      .join(' ')
      .toLowerCase()
      .includes(q)
  })
})

const selectedItem = computed<SkillItem | undefined>(
  () =>
    detail.value ??
    (catalog.value?.Skills ?? []).find((s) => s.ID === selectedId.value),
)

const activeCount = computed(
  () => (catalog.value?.Skills ?? []).filter((s) => s.Status === 'ACTIVE').length,
)
const draftCount = computed(
  () => (catalog.value?.Skills ?? []).filter((s) => s.HasDraft).length,
)
const disabledCount = computed(
  () =>
    (catalog.value?.Skills ?? []).filter((s) => s.Status === 'DISABLED').length,
)

const canCommitUpload = computed(
  () =>
    Boolean(review.value) &&
    (review.value!.Existing ||
      Boolean(
        uploadScope.value &&
          uploadDisplay.value.trim() &&
          uploadExample.value.trim(),
      )),
)

function showToast(msg: string) {
  toast.value = msg
  window.setTimeout(() => {
    if (toast.value === msg) toast.value = ''
  }, 4000)
}

async function runAction(fn: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  emit('busy', true)
  error.value = ''
  try {
    await fn()
  } catch (e) {
    error.value = String(e)
  } finally {
    busy.value = false
    emit('busy', false)
  }
}

async function loadSkills(keepSelected = true) {
  if (!currentEnv.value?.LoggedIn) {
    catalog.value = undefined
    detail.value = undefined
    selectedId.value = ''
    return
  }
  loading.value = true
  error.value = ''
  try {
    const res = await api().ListSkills(currentEnv.value.Name)
    catalog.value = res
    const visible = filteredSkills.value
    if (
      keepSelected &&
      selectedId.value &&
      res.Skills.some((s) => s.ID === selectedId.value)
    ) {
      await loadDetail(selectedId.value)
    } else if (visible.length > 0) {
      selectedId.value = visible[0]!.ID
      await loadDetail(selectedId.value)
    } else if (res.Skills.length > 0) {
      selectedId.value = res.Skills[0]!.ID
      await loadDetail(selectedId.value)
    } else {
      selectedId.value = ''
      detail.value = undefined
    }
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

async function loadDetail(id: string) {
  if (!currentEnv.value?.LoggedIn || !id) return
  selectedId.value = id
  detailLoading.value = true
  try {
    detail.value = await api().GetSkillDetail(currentEnv.value.Name, id)
  } catch (e) {
    error.value = String(e)
  } finally {
    detailLoading.value = false
  }
}

watch(
  () => [props.env, currentEnv.value?.LoggedIn, props.view.Stamp],
  () => {
    void loadSkills(true)
  },
  { immediate: true },
)

async function openDialog(mode: 'upload' | 'edit') {
  modalMode.value = mode
  await nextTick()
  if (!dialog.value?.open) dialog.value?.showModal()
  dialog.value?.focus()
}

async function closeDialog() {
  if (busy.value) return
  if (modalMode.value === 'upload' && review.value) {
    await api().Cancel().catch(() => {})
  }
  dialog.value?.close()
  modalMode.value = ''
  review.value = undefined
  receipt.value = undefined
}

async function startUploadFromPicker() {
  if (!currentEnv.value?.LoggedIn) {
    if (props.env) emit('login', props.env)
    return
  }
  await runAction(async () => {
    const f = await api().ChooseUpload('skill')
    if (!f?.Name) return
    const checked = await api().InspectUpload(
      props.view.Stamp,
      'skill',
      currentEnv.value!.Name,
      { Name: '', Password: '' },
    )
    prepareUploadModal(checked)
    await openDialog('upload')
  })
}

async function startUploadFromDirPicker() {
  if (!currentEnv.value?.LoggedIn) {
    if (props.env) emit('login', props.env)
    return
  }
  await runAction(async () => {
    const f = await api().ChooseSkillDir()
    if (!f?.Name) return
    const checked = await api().InspectUpload(
      props.view.Stamp,
      'skill',
      currentEnv.value!.Name,
      { Name: '', Password: '' },
    )
    prepareUploadModal(checked)
    await openDialog('upload')
  })
}

async function readDroppedDirectory(
  dirEntry: FileSystemDirectoryEntry,
  prefix = '',
): Promise<SkillDirEntry[]> {
  const reader = dirEntry.createReader()
  const children: FileSystemEntry[] = []
  while (true) {
    const batch = await new Promise<FileSystemEntry[]>((resolve, reject) =>
      reader.readEntries(resolve, reject),
    )
    if (!batch.length) break
    children.push(...batch)
  }
  const out: SkillDirEntry[] = []
  for (const child of children) {
    if (
      child.isDirectory &&
      (child.name === '.git' || child.name === '__MACOSX')
    ) {
      continue
    }
    if (
      child.isFile &&
      (child.name === '.DS_Store' || child.name.toLowerCase() === 'thumbs.db')
    ) {
      continue
    }
    const rel = prefix ? `${prefix}/${child.name}` : child.name
    if (child.isDirectory) {
      out.push(
        ...(await readDroppedDirectory(
          child as FileSystemDirectoryEntry,
          rel,
        )),
      )
    } else if (child.isFile) {
      const file = await new Promise<File>((resolve, reject) =>
        (child as FileSystemFileEntry).file(resolve, reject),
      )
      out.push({ Path: rel, ContentBase64: await fileToBase64(file) })
    }
  }
  return out
}

async function handleDrop(e: DragEvent) {
  dragging.value = false
  if (!currentEnv.value?.LoggedIn) return
  const firstItem = e.dataTransfer?.items?.[0]
  const entry = firstItem?.webkitGetAsEntry?.()
  if (entry?.isDirectory) {
    await runAction(async () => {
      const entries = await readDroppedDirectory(
        entry as FileSystemDirectoryEntry,
      )
      const checked = await api().InspectSkillDirUpload(
        props.view.Stamp,
        currentEnv.value!.Name,
        entry.name,
        entries,
      )
      prepareUploadModal(checked)
      await openDialog('upload')
    })
    return
  }
  const f = e.dataTransfer?.files?.[0]
  if (!f) return
  await runAction(async () => {
    const b64 = await fileToBase64(f)
    const checked = await api().InspectSkillUpload(
      props.view.Stamp,
      currentEnv.value!.Name,
      f.name,
      b64,
    )
    prepareUploadModal(checked)
    await openDialog('upload')
  })
}

function prepareUploadModal(checked: UploadReview) {
  review.value = checked
  receipt.value = undefined
  uploadDisplay.value = checked.Display
  uploadExample.value = checked.Example
  uploadScope.value = ''
  uploadPublish.value = false
}

async function commitUpload() {
  if (!review.value || !canCommitUpload.value) return
  await runAction(async () => {
    const checked = review.value!
    const res = await api().CommitUpload(checked.ID, {
      Scope: uploadScope.value,
      Display: uploadDisplay.value,
      Example: uploadExample.value,
      Publish: uploadPublish.value,
    })
    review.value = undefined
    receipt.value = res
    await loadSkills(true)
    const matched = catalog.value?.Skills.find((s) => s.Key === checked.Key)
    if (matched) {
      await loadDetail(matched.ID)
    }
  })
}

async function downloadVersion(versionId: string, versionNumber: number) {
  const s = selectedItem.value
  if (!s || !currentEnv.value) return
  await runAction(async () => {
    const summary = await api().DownloadSkillVersion(
      currentEnv.value!.Name,
      s.ID,
      versionId,
      `${s.Key}-v${versionNumber}.zip`,
    )
    if (summary) showToast(summary)
  })
}

async function publishDraft() {
  const s = selectedItem.value
  if (!s || !currentEnv.value || !s.HasDraft) return
  await runAction(async () => {
    await api().PublishSkillDraft(currentEnv.value!.Name, s.ID, s.Revision)
    showToast(`已发布 ${s.Display} 最新草稿`)
    await loadSkills(true)
  })
}

async function toggleStatus() {
  const s = selectedItem.value
  if (!s || !currentEnv.value) return
  const enable = s.Status !== 'ACTIVE'
  await runAction(async () => {
    await api().SetSkillStatus(currentEnv.value!.Name, s.ID, s.Revision, enable)
    showToast(enable ? `已启用 ${s.Display}` : `已停用 ${s.Display}`)
    await loadSkills(true)
  })
}

async function openEditMetadata() {
  const s = selectedItem.value
  if (!s) return
  editDisplay.value = s.Display
  editExample.value = s.Example
  editScope.value = s.ScopeIDs.length === 1 ? s.ScopeIDs[0]! : '*'
  await openDialog('edit')
}

async function saveMetadata() {
  const s = selectedItem.value
  if (!s || !currentEnv.value) return
  await runAction(async () => {
    await api().UpdateSkillMetadata(currentEnv.value!.Name, s.ID, {
      Display: editDisplay.value.trim(),
      Example: editExample.value.trim(),
      ScopeIDs: editScope.value === '*' ? [] : [editScope.value],
      Tags: s.Tags ?? [],
      ExpectedRevision: s.Revision,
    })
    dialog.value?.close()
    modalMode.value = ''
    showToast(`已保存 ${editDisplay.value.trim()} 的基本信息`)
    await loadSkills(true)
  })
}

function openAdminPage() {
  const url = currentEnv.value?.URL
  if (!url) return
  window.runtime?.BrowserOpenURL(url.replace(/\/$/, '') + '/admin/')
}

function formatDate(iso: string) {
  if (!iso) return ''
  return iso.slice(0, 16).replace('T', ' ')
}
</script>
<template>
  <div
    class="manager-view"
    @dragover.prevent="currentEnv?.LoggedIn && (dragging = true)"
    @dragleave.self="dragging = false"
    @drop.prevent="handleDrop"
  >
    <div v-if="dragging" class="drop-overlay">
      <div class="drop-overlay-box">
        <FileUp :size="26" :stroke-width="1.8" />
        <strong>释放文件夹或文件以上传 / 更新 Skill</strong>
        <small>支持 Skill 文件夹（自动打包）、SKILL.md（≤ 2 MiB）或 ZIP 包（≤ 15 MiB）</small>
      </div>
    </div>

    <header class="win-toolbar">
      <div class="win-title">
        <h1>企业Skill</h1>
        <span v-if="currentEnv" class="toolbar-env-badge">
          <span class="env-dot" :class="{ connected: currentEnv.LoggedIn }" />
          <span>{{ currentEnv.Name }}</span>
        </span>
        <span v-if="catalog" class="title-count"
          >{{ catalog.Skills.length }} 个</span
        >
      </div>
      <div class="win-controls">
        <label class="search">
          <Search :size="13" :stroke-width="2" />
          <input
            v-model="search"
            type="search"
            placeholder="搜索 Skill 名称或标识"
            aria-label="搜索 Skill 名称或标识"
          />
        </label>
        <button
          :disabled="busy || !currentEnv?.LoggedIn"
          title="选择 SKILL.md 或 ZIP 技能包上传"
          @click="startUploadFromPicker"
        >
          <Upload :size="13.5" :stroke-width="2" />上传文件
        </button>
        <button
          class="primary"
          :disabled="busy || !currentEnv?.LoggedIn"
          title="选择包含 SKILL.md 的本地目录，自动打包成 ZIP 上传"
          @click="startUploadFromDirPicker"
        >
          <FolderUp :size="13.5" :stroke-width="2" />上传目录
        </button>
        <button
          class="tool-icon-btn"
          :disabled="loading || busy || !currentEnv?.LoggedIn"
          aria-label="刷新 Skill 列表"
          title="刷新 Skill 列表"
          @click="loadSkills(true)"
        >
          <RefreshCw :size="14" :stroke-width="1.9" />
        </button>
      </div>
    </header>

    <p v-if="toast" class="toast-strip" role="status">{{ toast }}</p>
    <p v-if="error && !modalMode" class="error strip-error" role="alert">
      {{ error }}
    </p>

    <div v-if="!currentEnv" class="center-empty">
      <Layers3 :size="28" :stroke-width="1.6" />
      <p>尚未配置环境，请先在左侧添加环境。</p>
    </div>

    <div v-else-if="!currentEnv.LoggedIn" class="center-empty">
      <KeyRound :size="26" :stroke-width="1.7" />
      <strong>环境 {{ currentEnv.Name }} 尚未登录</strong>
      <p>{{ currentEnv.URL }} · 登录后即可查看、下载和更新企业 Skill</p>
      <button class="primary" @click="emit('login', currentEnv.Name)">
        立即登录 {{ currentEnv.Name }}
      </button>
    </div>

    <div v-else class="split-body">
      <section class="list-pane" aria-label="Skill 列表">
        <div class="pane-filter">
          <button
            v-for="f in [
              ['active', '有效', activeCount],
              ['draft', '草稿', draftCount],
              ['disabled', '停用', disabledCount],
              ['all', '全部', catalog?.Skills.length ?? 0],
            ] as const"
            :key="f[0]"
            :aria-pressed="filter === f[0]"
            @click="filter = f[0]"
          >
            {{ f[1] }} <small>{{ f[2] }}</small>
          </button>
        </div>

        <div class="list-scroll">
          <div v-if="loading && !catalog" class="list-placeholder">
            正在拉取 Skill 列表…
          </div>
          <div v-else-if="!filteredSkills.length" class="list-placeholder">
            {{
              catalog?.Skills.length
                ? '没有匹配的 Skill'
                : '当前环境还没有企业 Skill，可拖入 SKILL.md 或 ZIP 上传。'
            }}
          </div>
          <button
            v-for="s in filteredSkills"
            :key="s.ID"
            class="master-item"
            :class="{ active: selectedId === s.ID }"
            @click="loadDetail(s.ID)"
          >
            <div class="master-item-top">
              <strong>{{ s.Display }}</strong>
              <span
                class="status"
                :data-tone="
                  s.Status === 'DISABLED'
                    ? 'error'
                    : s.HasDraft
                      ? 'warning'
                      : 'success'
                "
              >
                {{
                  s.Status === 'DISABLED'
                    ? '已停用'
                    : s.HasDraft
                      ? '有草稿'
                      : s.CurrentNumber
                        ? 'v' + s.CurrentNumber
                        : '未发布'
                }}
              </span>
            </div>
            <div class="master-item-sub">
              <code>{{ s.Key }}</code>
              <span>{{
                s.ScopeNames.length ? s.ScopeNames.join('、') : '通用'
              }}</span>
            </div>
          </button>
        </div>
      </section>

      <section class="detail-pane" aria-label="Skill 详情">
        <div v-if="!selectedItem" class="detail-empty">
          <p>在左侧选择一个 Skill 查看详情，或将 SKILL.md / ZIP 拖入窗口直接上传。</p>
        </div>
        <div v-else class="detail-inner">
          <div class="detail-header">
            <div class="detail-title-block">
              <div class="detail-title-row">
                <h2>{{ selectedItem.Display }}</h2>
                <span
                  class="status"
                  :data-tone="
                    selectedItem.Status === 'DISABLED' ? 'error' : 'success'
                  "
                >
                  {{ selectedItem.Status === 'DISABLED' ? '已停用' : '运行中' }}
                </span>
                <span
                  v-if="selectedItem.HasDraft"
                  class="status"
                  data-tone="warning"
                >
                  待发布草稿 v{{ selectedItem.DraftNumber }}
                </span>
              </div>
              <div class="detail-sub-row">
                <code>{{ selectedItem.Key }}</code>
                <span class="muted"
                  >正式版：{{
                    selectedItem.CurrentNumber
                      ? 'v' + selectedItem.CurrentNumber
                      : '尚未发布'
                  }}</span
                >
                <span class="muted">修订 r{{ selectedItem.Revision }}</span>
              </div>
            </div>
            <div class="detail-actions">
              <button
                v-if="
                  selectedItem.CurrentVersionID || selectedItem.DraftVersionID
                "
                :disabled="busy"
                @click="
                  downloadVersion(
                    selectedItem.DraftVersionID ||
                      selectedItem.CurrentVersionID,
                    selectedItem.DraftNumber || selectedItem.CurrentNumber || 1,
                  )
                "
              >
                <Download :size="13.5" :stroke-width="2" />下载原包
              </button>
              <button
                :disabled="busy"
                title="选择 Skill 目录自动打包更新"
                @click="startUploadFromDirPicker"
              >
                <FolderUp :size="13.5" :stroke-width="2" />目录更新
              </button>
              <button
                :disabled="busy"
                title="选择 SKILL.md 或 ZIP 包更新"
                @click="startUploadFromPicker"
              >
                <Upload :size="13.5" :stroke-width="2" />文件更新
              </button>
              <button
                v-if="selectedItem.HasDraft && selectedItem.Status === 'ACTIVE'"
                class="primary"
                :disabled="busy"
                @click="publishDraft"
              >
                <Rocket :size="13.5" :stroke-width="2" />发布草稿
              </button>
              <button
                v-if="selectedItem.Status === 'ACTIVE'"
                :disabled="busy"
                @click="openEditMetadata"
              >
                <Pencil :size="13" :stroke-width="2" />编辑信息
              </button>
              <button
                class="ghost-btn"
                :class="{ 'danger-text': selectedItem.Status === 'ACTIVE' }"
                :disabled="busy"
                @click="toggleStatus"
              >
                <Power :size="13.5" :stroke-width="2" />{{
                  selectedItem.Status === 'ACTIVE' ? '停用' : '启用'
                }}
              </button>
              <button
                class="tool-icon-btn"
                title="在浏览器打开管理台"
                aria-label="在浏览器打开管理台"
                @click="openAdminPage"
              >
                <ExternalLink :size="13.5" :stroke-width="2" />
              </button>
            </div>
          </div>

          <dl class="manifest-bar three">
            <div>
              <dt>适用业务系统</dt>
              <dd>
                <strong>{{
                  selectedItem.ScopeNames.length
                    ? selectedItem.ScopeNames.join('、')
                    : '通用（全部业务系统）'
                }}</strong>
              </dd>
            </div>
            <div>
              <dt>当前正式版</dt>
              <dd>
                <strong>{{
                  selectedItem.CurrentNumber
                    ? 'v' + selectedItem.CurrentNumber
                    : '未发布'
                }}</strong>
              </dd>
            </div>
            <div>
              <dt>最近更新</dt>
              <dd class="muted">
                {{ formatDate(selectedItem.UpdatedAt) || '—' }}
              </dd>
            </div>
          </dl>

          <div class="detail-section">
            <h3>示例提问</h3>
            <p class="detail-box">{{ selectedItem.Example || '未设置' }}</p>
          </div>

          <div v-if="selectedItem.Description" class="detail-section">
            <h3>技能说明</h3>
            <p class="detail-box">{{ selectedItem.Description }}</p>
          </div>

          <div class="detail-section">
            <div class="detail-section-head">
              <h3>版本记录</h3>
              <span v-if="detailLoading" class="muted">加载中…</span>
            </div>
            <div v-if="detail?.Versions?.length" class="version-list">
              <div
                v-for="ver in detail.Versions"
                :key="ver.VersionID"
                class="version-row"
              >
                <div class="version-info">
                  <strong>v{{ ver.VersionNumber }}</strong>
                  <span
                    v-if="ver.VersionID === selectedItem.CurrentVersionID"
                    class="status"
                    data-tone="success"
                    >当前正式</span
                  >
                  <span
                    v-else-if="ver.VersionID === selectedItem.DraftVersionID"
                    class="status"
                    data-tone="warning"
                    >当前草稿</span
                  >
                  <span class="muted">{{ formatDate(ver.CreatedAt) }}</span>
                </div>
                <button
                  class="compact-btn"
                  :disabled="busy"
                  @click="downloadVersion(ver.VersionID, ver.VersionNumber)"
                >
                  <Download :size="12" :stroke-width="2" />下载
                </button>
              </div>
            </div>
            <p v-else class="muted">暂无版本记录</p>
          </div>
        </div>
      </section>
    </div>

    <dialog
      ref="dialog"
      tabindex="-1"
      aria-labelledby="skill-modal-title"
      @cancel.prevent="closeDialog"
    >
      <div class="dialog-head">
        <h2 id="skill-modal-title">
          {{
            modalMode === 'edit'
              ? '编辑 Skill 信息'
              : receipt
                ? '上传回执'
                : '核对 Skill 上传'
          }}
        </h2>
        <button
          class="close"
          aria-label="关闭"
          :disabled="busy"
          @click="closeDialog"
        >
          <X :size="15" :stroke-width="2" />
        </button>
      </div>

      <template v-if="modalMode === 'upload' && review">
        <div class="body">
          <div v-if="isProd" class="callout warning">
            <AlertTriangle :size="14" :stroke-width="2" />
            <p>
              当前目标 <strong>{{ review.Env }}</strong> 标记为正式环境，请仔细核对后再提交。
            </p>
          </div>

          <dl class="manifest-bar three">
            <div>
              <dt>目标环境</dt>
              <dd><span class="env-pill">{{ review.Env }}</span></dd>
            </div>
            <div>
              <dt>上传文件</dt>
              <dd>
                <strong>{{ review.File.Name }}</strong>
                <span class="muted">{{ Math.ceil(review.File.Bytes / 1024) }} KiB</span>
              </dd>
            </div>
            <div>
              <dt>技能标识</dt>
              <dd><code>{{ review.Key }}</code></dd>
            </div>
          </dl>

          <div class="inset-group" style="margin-top: 12px">
            <div v-if="review.Existing" class="callout">
              <Info :size="14" :stroke-width="2" />
              <p>
                将更新已有 Skill <strong>{{ review.Target }}</strong
                >（当前修订 r{{ review.Revision }}），保留原有适用范围。
              </p>
            </div>
            <template v-else>
              <div class="callout">
                <Info :size="14" :stroke-width="2" />
                <p>此环境将新建 Skill。</p>
              </div>
              <div class="form-grid stack-row">
                <label>
                  适用业务系统
                  <select v-model="uploadScope" :disabled="busy">
                    <option value="" disabled>选择适用范围</option>
                    <option
                      v-for="o in review.Options"
                      :key="o.ID"
                      :value="o.ID"
                    >
                      {{ o.Name }}
                    </option>
                    <option value="*">通用（全部业务系统）</option>
                  </select>
                </label>
                <label>
                  显示名称
                  <input
                    v-model="uploadDisplay"
                    required
                    maxlength="128"
                    :disabled="busy"
                  />
                </label>
              </div>
              <label class="stack-label">
                示例提问
                <textarea
                  v-model="uploadExample"
                  required
                  rows="2"
                  maxlength="2000"
                  :disabled="busy"
                />
              </label>
            </template>

            <label class="stack-label">
              保存方式
              <select v-model="uploadPublish" :disabled="busy">
                <option :value="false">只保存草稿</option>
                <option :value="true">保存后立即发布</option>
              </select>
            </label>
          </div>
          <p v-if="error" class="error" role="alert">{{ error }}</p>
        </div>
        <div class="dialog-foot">
          <button :disabled="busy" @click="closeDialog">取消</button>
          <button
            class="primary"
            :disabled="busy || !canCommitUpload"
            @click="commitUpload"
          >
            {{
              busy
                ? '正在提交…'
                : uploadPublish
                  ? '确认保存并发布Skill'
                  : '确认保存Skill草稿'
            }}
          </button>
        </div>
      </template>

      <template v-else-if="modalMode === 'upload' && receipt">
        <div class="body">
          <div
            class="receipt-sheet"
            :data-tone="statusTone(receipt.Status)"
            aria-live="polite"
          >
            <div class="receipt-head">
              <span class="receipt-icon">
                <CheckCircle2 :size="19" :stroke-width="2" />
              </span>
              <div>
                <h2>{{ receipt.Status }}</h2>
                <p class="receipt-sub">{{ currentEnv?.Name }}</p>
              </div>
            </div>
            <p class="receipt-detail">{{ receipt.Detail }}</p>
            <p v-if="receipt.Warning" class="notice">{{ receipt.Warning }}</p>
          </div>
        </div>
        <div class="dialog-foot">
          <button class="primary" @click="closeDialog">完成</button>
        </div>
      </template>

      <form
        v-else-if="modalMode === 'edit'"
        @submit.prevent="saveMetadata"
      >
        <div class="body">
          <div class="inset-group">
            <label class="stack-label">
              显示名称
              <input
                v-model="editDisplay"
                required
                maxlength="128"
                :disabled="busy"
              />
            </label>
            <label class="stack-label">
              适用业务系统
              <select v-model="editScope" :disabled="busy">
                <option value="*">通用（全部业务系统）</option>
                <option
                  v-for="s in catalog?.Systems"
                  :key="s.ID"
                  :value="s.ID"
                >
                  {{ s.Name }}
                </option>
              </select>
            </label>
            <label class="stack-label">
              示例提问
              <textarea
                v-model="editExample"
                required
                rows="3"
                maxlength="2000"
                :disabled="busy"
              />
            </label>
          </div>
          <p v-if="error" class="error" role="alert">{{ error }}</p>
        </div>
        <div class="dialog-foot">
          <button type="button" :disabled="busy" @click="closeDialog">
            取消
          </button>
          <button class="primary" :disabled="busy">
            {{ busy ? '保存中…' : '保存修改' }}
          </button>
        </div>
      </form>
    </dialog>
  </div>
</template>
