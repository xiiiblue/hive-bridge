<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import {
  Search,
  RefreshCw,
  Upload,
  Download,
  RotateCcw,
  Trash2,
  Plus,
  Pencil,
  KeyRound,
  BookOpen,
  FileUp,
  X,
  AlertTriangle,
  CheckCircle2,
} from 'lucide-vue-next'
import {
  api,
  fileToBase64,
  type BatchUploadItem,
  type BatchUploadResult,
  type KnowledgeBaseItem,
  type KnowledgeDocItem,
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

const bases = ref<KnowledgeBaseItem[]>([])
const selectedBaseId = ref('')
const docs = ref<KnowledgeDocItem[]>([])
const loadingBases = ref(false)
const loadingDocs = ref(false)
const busy = ref(false)
const error = ref('')
const toast = ref('')
const search = ref('')
const dragging = ref(false)

// 弹窗：kb-form | batch-upload | batch-results
const dialog = ref<HTMLDialogElement>()
const modalMode = ref<'kb-form' | 'batch-upload' | 'batch-results' | ''>('')
const kbEditing = ref<KnowledgeBaseItem | null>(null)
const kbName = ref('')
const kbDesc = ref('')

const pendingFiles = ref<
  {
    Filename: string
    ContentBase64: string
    Bytes: number
    ExistingRev: number
  }[]
>([])
const batchResults = ref<BatchUploadResult[]>([])

const currentEnv = computed(() =>
  props.view.Environments.find((e) => e.Name === props.env),
)
const isProd = computed(() => currentEnv.value?.Tag === 'prod')

const selectedBase = computed(() =>
  bases.value.find((b) => b.knowledge_base_id === selectedBaseId.value),
)

const filteredDocs = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return docs.value
  return docs.value.filter((d) => d.filename.toLowerCase().includes(q))
})

function showToast(msg: string) {
  toast.value = msg
  window.setTimeout(() => {
    if (toast.value === msg) toast.value = ''
  }, 4000)
}

function docStatusLabel(status: string) {
  return (
    { READY: '可检索', PROCESSING: '处理中', FAILED: '处理失败' }[status] ||
    status
  )
}

function docStatusTone(status: string) {
  if (status === 'READY') return 'success'
  if (status === 'FAILED') return 'error'
  return 'warning'
}

function formatBytes(n: number) {
  if (!n) return '—'
  return n >= 1048576
    ? (n / 1048576).toFixed(1) + ' MiB'
    : Math.max(1, Math.ceil(n / 1024)) + ' KiB'
}

function fileExt(name: string) {
  const m = /\.([^.]+)$/.exec(name)
  return m ? m[1]!.toUpperCase() : 'DOC'
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

async function loadBases(keepSelected = true) {
  if (!currentEnv.value?.LoggedIn) {
    bases.value = []
    docs.value = []
    selectedBaseId.value = ''
    return
  }
  loadingBases.value = true
  error.value = ''
  try {
    const items = await api().ListKnowledgeBases(currentEnv.value.Name)
    bases.value = items
    if (
      keepSelected &&
      selectedBaseId.value &&
      items.some((b) => b.knowledge_base_id === selectedBaseId.value)
    ) {
      await loadDocs(selectedBaseId.value)
    } else if (items.length > 0) {
      selectedBaseId.value = items[0]!.knowledge_base_id
      await loadDocs(selectedBaseId.value)
    } else {
      selectedBaseId.value = ''
      docs.value = []
    }
  } catch (e) {
    error.value = String(e)
  } finally {
    loadingBases.value = false
  }
}

async function loadDocs(baseId: string) {
  if (!currentEnv.value?.LoggedIn || !baseId) return
  selectedBaseId.value = baseId
  loadingDocs.value = true
  try {
    docs.value = await api().ListKnowledgeDocuments(
      currentEnv.value.Name,
      baseId,
    )
  } catch (e) {
    error.value = String(e)
  } finally {
    loadingDocs.value = false
  }
}

watch(
  () => [props.env, currentEnv.value?.LoggedIn, props.view.Stamp],
  () => {
    void loadBases(true)
  },
  { immediate: true },
)

async function openDialog(mode: 'kb-form' | 'batch-upload' | 'batch-results') {
  modalMode.value = mode
  await nextTick()
  if (!dialog.value?.open) dialog.value?.showModal()
  if (mode === 'kb-form') {
    dialog.value?.querySelector<HTMLInputElement>('input')?.focus()
  } else {
    dialog.value?.focus()
  }
}

function closeDialog() {
  if (busy.value) return
  dialog.value?.close()
  modalMode.value = ''
  pendingFiles.value = []
  batchResults.value = []
}

async function openNewBase() {
  kbEditing.value = null
  kbName.value = ''
  kbDesc.value = ''
  error.value = ''
  await openDialog('kb-form')
}

async function openEditBase(kb: KnowledgeBaseItem) {
  kbEditing.value = kb
  kbName.value = kb.name
  kbDesc.value = kb.description
  error.value = ''
  await openDialog('kb-form')
}

async function saveBase() {
  if (!currentEnv.value) return
  await runAction(async () => {
    await api().SaveKnowledgeBase(
      currentEnv.value!.Name,
      kbEditing.value?.knowledge_base_id ?? '',
      kbName.value.trim(),
      kbDesc.value.trim(),
      kbEditing.value?.revision ?? 0,
    )
    closeDialog()
    showToast(kbEditing.value ? '已更新知识库信息' : '已新建知识库')
    await loadBases(true)
  })
}

async function removeBase(kb: KnowledgeBaseItem) {
  if (!currentEnv.value) return
  await runAction(async () => {
    await api().DeleteKnowledgeBase(
      currentEnv.value!.Name,
      kb.knowledge_base_id,
      kb.revision,
    )
    showToast(`已删除知识库 ${kb.name}`)
    await loadBases(false)
  })
}

function prepareBatchFiles(items: BatchUploadItem[]) {
  const byName = new Map(docs.value.map((d) => [d.filename, d]))
  pendingFiles.value = items.map((item) => {
    const existing = byName.get(item.Filename)
    return {
      Filename: item.Filename,
      ContentBase64: item.ContentBase64,
      Bytes: Math.floor((item.ContentBase64.length * 3) / 4),
      ExistingRev: existing ? existing.revision : 0,
    }
  })
}

async function startUploadFromPicker() {
  if (!currentEnv.value?.LoggedIn || !selectedBase.value) return
  await runAction(async () => {
    const items = await api().ChooseKnowledgeFiles()
    if (!items || !items.length) return
    prepareBatchFiles(items)
    await openDialog('batch-upload')
  })
}

async function handleDrop(e: DragEvent) {
  dragging.value = false
  if (!currentEnv.value?.LoggedIn || !selectedBase.value) return
  const files = Array.from(e.dataTransfer?.files ?? [])
  if (!files.length) return
  await runAction(async () => {
    const items: BatchUploadItem[] = []
    for (const f of files) {
      items.push({ Filename: f.name, ContentBase64: await fileToBase64(f) })
    }
    prepareBatchFiles(items)
    await openDialog('batch-upload')
  })
}

async function confirmBatchUpload() {
  if (!currentEnv.value || !selectedBase.value || !pendingFiles.value.length)
    return
  await runAction(async () => {
    const res = await api().UploadKnowledgeBatch(
      currentEnv.value!.Name,
      selectedBase.value!.knowledge_base_id,
      pendingFiles.value.map(({ Filename, ContentBase64 }) => ({
        Filename,
        ContentBase64,
      })),
    )
    batchResults.value = res
    modalMode.value = 'batch-results'
    await loadBases(true)
  })
}

async function downloadDoc(doc: KnowledgeDocItem) {
  if (!currentEnv.value || !selectedBase.value) return
  await runAction(async () => {
    const summary = await api().DownloadKnowledgeDocument(
      currentEnv.value!.Name,
      selectedBase.value!.knowledge_base_id,
      doc.document_id,
      doc.filename,
    )
    if (summary) showToast(summary)
  })
}

async function retryDoc(doc: KnowledgeDocItem) {
  if (!currentEnv.value || !selectedBase.value) return
  await runAction(async () => {
    await api().RetryKnowledgeDocument(
      currentEnv.value!.Name,
      selectedBase.value!.knowledge_base_id,
      doc.document_id,
      doc.revision,
    )
    showToast(`已重新处理 ${doc.filename}`)
    await loadDocs(selectedBase.value!.knowledge_base_id)
  })
}

async function deleteDoc(doc: KnowledgeDocItem) {
  if (!currentEnv.value || !selectedBase.value) return
  await runAction(async () => {
    await api().DeleteKnowledgeDocument(
      currentEnv.value!.Name,
      selectedBase.value!.knowledge_base_id,
      doc.document_id,
      doc.revision,
    )
    showToast(`已删除 ${doc.filename}`)
    await loadBases(true)
  })
}
</script>
<template>
  <div
    class="manager-view"
    @dragover.prevent="
      currentEnv?.LoggedIn && selectedBase && (dragging = true)
    "
    @dragleave.self="dragging = false"
    @drop.prevent="handleDrop"
  >
    <div v-if="dragging" class="drop-overlay">
      <div class="drop-overlay-box">
        <FileUp :size="26" :stroke-width="1.8" />
        <strong>释放文件批量上传到「{{ selectedBase?.name }}」</strong>
        <small
          >支持 Markdown、DOCX 或可提取文字的 PDF（单文件 ≤ 10 MiB）</small
        >
      </div>
    </div>

    <header class="win-toolbar">
      <div class="win-title">
        <h1>知识库文档</h1>
        <span v-if="currentEnv" class="toolbar-env-badge">
          <span class="env-dot" :class="{ connected: currentEnv.LoggedIn }" />
          <span>{{ currentEnv.Name }}</span>
        </span>
        <span v-if="bases.length" class="title-count"
          >{{ bases.length }} 个库</span
        >
      </div>
      <div class="win-controls">
        <label class="search">
          <Search :size="13" :stroke-width="2" />
          <input
            v-model="search"
            type="search"
            placeholder="搜索文档文件名"
            aria-label="搜索文档文件名"
          />
        </label>
        <button
          :disabled="busy || !currentEnv?.LoggedIn"
          @click="openNewBase"
        >
          <Plus :size="13.5" :stroke-width="2" />新建知识库
        </button>
        <button
          class="primary"
          :disabled="busy || !currentEnv?.LoggedIn || !selectedBase"
          @click="startUploadFromPicker"
        >
          <Upload :size="13.5" :stroke-width="2" />上传文档
        </button>
        <button
          class="tool-icon-btn"
          :disabled="loadingBases || busy || !currentEnv?.LoggedIn"
          aria-label="刷新知识库"
          title="刷新知识库"
          @click="loadBases(true)"
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
      <BookOpen :size="28" :stroke-width="1.6" />
      <p>尚未配置环境，请先在左侧添加环境。</p>
    </div>

    <div v-else-if="!currentEnv.LoggedIn" class="center-empty">
      <KeyRound :size="26" :stroke-width="1.7" />
      <strong>环境 {{ currentEnv.Name }} 尚未登录</strong>
      <p>{{ currentEnv.URL }} · 登录后即可浏览知识库并批量上传或下载文档</p>
      <button class="primary" @click="emit('login', currentEnv.Name)">
        立即登录 {{ currentEnv.Name }}
      </button>
    </div>

    <div v-else class="split-body kb-split">
      <section class="list-pane kb-list-pane" aria-label="知识库列表">
        <div class="list-scroll">
          <div v-if="loadingBases && !bases.length" class="list-placeholder">
            正在拉取知识库…
          </div>
          <div v-else-if="!bases.length" class="list-placeholder">
            此环境还没有知识库，可点击右上角“新建知识库”。
          </div>
          <div
            v-for="kb in bases"
            :key="kb.knowledge_base_id"
            class="kb-item"
            :class="{ active: selectedBaseId === kb.knowledge_base_id }"
            @click="loadDocs(kb.knowledge_base_id)"
          >
            <div class="kb-item-main">
              <strong>{{ kb.name }}</strong>
              <span class="nav-badge">{{ kb.document_count }}</span>
            </div>
            <p v-if="kb.description" class="kb-desc">{{ kb.description }}</p>
            <div class="kb-item-actions" @click.stop>
              <button
                class="tool-icon-btn mini"
                title="编辑知识库"
                aria-label="编辑知识库"
                :disabled="busy"
                @click="openEditBase(kb)"
              >
                <Pencil :size="12" :stroke-width="2" />
              </button>
              <button
                v-if="kb.document_count === 0 && kb.group_count === 0"
                class="tool-icon-btn mini danger-text"
                title="删除空知识库"
                aria-label="删除空知识库"
                :disabled="busy"
                @click="removeBase(kb)"
              >
                <Trash2 :size="12" :stroke-width="2" />
              </button>
            </div>
          </div>
        </div>
      </section>

      <section class="docs-pane" aria-label="知识库文档列表">
        <div v-if="!selectedBase" class="detail-empty">
          <p>在左侧选择一个知识库查看文档。</p>
        </div>
        <template v-else>
          <div class="kb-bar">
            <div class="kb-bar-title">
              <strong>{{ selectedBase.name }}</strong>
              <span class="muted">
                {{ docs.length }} 份文档 · {{ selectedBase.ready_count }} 份可检索
              </span>
            </div>
            <span class="muted kb-drop-tip"
              >可直接拖入 Markdown / DOCX / PDF 批量上传</span
            >
          </div>

          <div class="docs-scroll">
            <div v-if="loadingDocs && !docs.length" class="list-placeholder">
              正在加载文档…
            </div>
            <div v-else-if="!filteredDocs.length" class="detail-empty">
              <p>
                {{
                  docs.length
                    ? '没有匹配的文档'
                    : '此知识库暂无文档，点击右上角“上传文档”或直接将文件拖入此区域。'
                }}
              </p>
            </div>
            <table v-else class="docs-table">
              <thead>
                <tr>
                  <th scope="col" class="col-panel">文件名</th>
                  <th scope="col">大小</th>
                  <th scope="col">修订</th>
                  <th scope="col">状态</th>
                  <th scope="col" class="col-actions">操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="doc in filteredDocs" :key="doc.document_id">
                  <td>
                    <div class="doc-name-cell">
                      <span class="ext-badge">{{ fileExt(doc.filename) }}</span>
                      <div class="doc-name-copy">
                        <strong>{{ doc.filename }}</strong>
                        <small v-if="doc.error_code" class="danger-text">{{
                          doc.error_code
                        }}</small>
                        <small v-else-if="doc.warning" class="muted">{{
                          doc.warning
                        }}</small>
                      </div>
                    </div>
                  </td>
                  <td class="muted">{{ formatBytes(doc.byte_size) }}</td>
                  <td class="muted">r{{ doc.revision }}</td>
                  <td>
                    <span
                      class="status"
                      :data-tone="docStatusTone(doc.status)"
                      >{{ docStatusLabel(doc.status) }}</span
                    >
                  </td>
                  <td class="col-actions">
                    <div class="row-actions">
                      <button
                        class="compact-btn"
                        :disabled="busy"
                        @click="downloadDoc(doc)"
                      >
                        <Download :size="12" :stroke-width="2" />下载
                      </button>
                      <button
                        v-if="doc.status === 'FAILED'"
                        class="compact-btn"
                        :disabled="busy"
                        @click="retryDoc(doc)"
                      >
                        <RotateCcw :size="12" :stroke-width="2" />重试
                      </button>
                      <button
                        class="tool-icon-btn mini danger-text"
                        :disabled="busy"
                        :aria-label="'删除 ' + doc.filename"
                        title="删除文档"
                        @click="deleteDoc(doc)"
                      >
                        <Trash2 :size="13" :stroke-width="2" />
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
      </section>
    </div>

    <dialog
      ref="dialog"
      tabindex="-1"
      aria-labelledby="kb-modal-title"
      @cancel.prevent="closeDialog"
    >
      <div class="dialog-head">
        <h2 id="kb-modal-title">
          {{
            modalMode === 'kb-form'
              ? kbEditing
                ? '编辑知识库'
                : '新建知识库'
              : modalMode === 'batch-results'
                ? '文档上传结果'
                : '确认上传文档'
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

      <form v-if="modalMode === 'kb-form'" @submit.prevent="saveBase">
        <div class="body">
          <div class="inset-group">
            <label class="stack-label">
              知识库名称
              <input
                v-model="kbName"
                required
                maxlength="120"
                placeholder="例如 运维操作手册"
                :disabled="busy"
              />
            </label>
            <label class="stack-label">
              简介（可选）
              <textarea
                v-model="kbDesc"
                rows="2"
                maxlength="500"
                placeholder="说明该知识库包含哪些文档"
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
            {{ busy ? '保存中…' : kbEditing ? '保存修改' : '创建知识库' }}
          </button>
        </div>
      </form>

      <template v-else-if="modalMode === 'batch-upload'">
        <div class="body">
          <div v-if="isProd" class="callout warning">
            <AlertTriangle :size="14" :stroke-width="2" />
            <p>
              当前环境 <strong>{{ currentEnv?.Name }}</strong> 标记为正式环境，请核对文件清单后再确认上传。
            </p>
          </div>
          <p class="hint">
            目标知识库：<strong>{{ selectedBase?.name }}</strong>（{{
              currentEnv?.Name
            }}）
          </p>
          <div class="result-group">
            <article v-for="(item, idx) in pendingFiles" :key="item.Filename + idx">
              <div class="result-title">
                <div class="result-name">
                  <strong>{{ item.Filename }}</strong>
                  <span class="muted">{{ formatBytes(item.Bytes) }}</span>
                </div>
                <b
                  class="status"
                  :data-tone="item.ExistingRev ? 'warning' : 'success'"
                >
                  {{
                    item.ExistingRev
                      ? '替换同名 · 当前 r' + item.ExistingRev
                      : '新增文档'
                  }}
                </b>
              </div>
            </article>
          </div>
          <p v-if="error" class="error" role="alert">{{ error }}</p>
        </div>
        <div class="dialog-foot">
          <button :disabled="busy" @click="closeDialog">取消</button>
          <button
            class="primary"
            :disabled="busy || !pendingFiles.length"
            @click="confirmBatchUpload"
          >
            {{
              busy
                ? '正在上传…'
                : '确认上传 ' + pendingFiles.length + ' 份文档'
            }}
          </button>
        </div>
      </template>

      <template v-else-if="modalMode === 'batch-results'">
        <div class="body">
          <div class="result-group">
            <article v-for="r in batchResults" :key="r.Filename">
              <div class="result-title">
                <div class="result-name">
                  <CheckCircle2
                    v-if="r.Status === '可检索'"
                    :size="14"
                    class="success-icon"
                  />
                  <strong>{{ r.Filename }}</strong>
                  <span v-if="r.Replaced" class="env-pill">已替换</span>
                </div>
                <b class="status" :data-tone="statusTone(r.Status)">{{
                  r.Status
                }}</b>
              </div>
              <p v-if="r.Detail" class="result-detail">{{ r.Detail }}</p>
            </article>
          </div>
        </div>
        <div class="dialog-foot">
          <button class="primary" @click="closeDialog">完成</button>
        </div>
      </template>
    </dialog>
  </div>
</template>
