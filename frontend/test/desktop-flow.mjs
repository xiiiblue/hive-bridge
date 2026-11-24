import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdir } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { chromium, webkit } from 'playwright'
import { preview } from 'vite'

// 合成环境只提供 Wails 调用返回值，桌面组件与交互使用实际构建产物。
function installFixture() {
  const panels = [
    ['overview', '运行概览', 'overview-dev', 'overview-test'],
    ['tickets', '工单列表', 'tickets', ''],
    ['dispatch', '任务调度', 'dispatch-dev', 'dispatch'],
    ['assets', '资产台账', 'asset-list', 'assets'],
    ['map', '设施地图', 'facility-map', 'map'],
    ['energy', '节能效果', 'energy', 'energy-test'],
  ].map(([Alias, Name, dev, test], index) => ({
    Alias,
    Name,
    Source: index < 4 ? 'panels/' + Alias : '',
    Bindings: {
      开发环境: 'ops/' + dev,
      ...(test ? { 联调环境: 'ops/' + test } : {}),
      预发环境: 'ops/' + dev,
      生产环境: 'ops/' + dev,
    },
  }))
  const savedCreds = {}
  const envList = [
    { Name: '开发环境', URL: 'https://dev.hive.example.com', Tag: 'dev', Order: 1 },
    { Name: '联调环境', URL: 'https://staging.hive.example.com', Tag: 'test', Order: 2 },
    { Name: '预发环境', URL: 'https://pre.hive.example.com', Tag: 'prod', Order: 3 },
    { Name: '生产环境', URL: 'https://prod.hive.example.com', Tag: 'prod', Order: 4 },
  ]
  const skills = [
    {
      ID: 'sk-1',
      Key: 'duty-report',
      Display: '值班报告助手',
      Description: '自动整理当班告警、工单与巡检摘要。',
      Example: '帮我整理今晚的值班报告',
      Status: 'ACTIVE',
      Revision: 3,
      CurrentVersionID: 'ver-2',
      CurrentNumber: 2,
      DraftVersionID: 'ver-3',
      DraftNumber: 3,
      HasDraft: true,
      Tags: ['运维'],
      ScopeIDs: ['ops'],
      ScopeNames: ['运维系统'],
      UpdatedAt: '2026-11-24T09:30:00Z',
      Versions: [
        { VersionID: 'ver-3', VersionNumber: 3, CreatedAt: '2026-11-24T09:30:00Z' },
        { VersionID: 'ver-2', VersionNumber: 2, CreatedAt: '2026-11-22T14:10:00Z' },
      ],
    },
    {
      ID: 'sk-2',
      Key: 'alarm-triage',
      Display: '告警分级研判',
      Description: '结合历史处置记录自动建议排查步骤。',
      Example: '分析当前高优告警原因',
      Status: 'ACTIVE',
      Revision: 1,
      CurrentVersionID: 'ver-1',
      CurrentNumber: 1,
      DraftVersionID: 'ver-1',
      DraftNumber: 1,
      HasDraft: false,
      Tags: [],
      ScopeIDs: [],
      ScopeNames: [],
      UpdatedAt: '2026-11-20T11:00:00Z',
      Versions: [
        { VersionID: 'ver-1', VersionNumber: 1, CreatedAt: '2026-11-20T11:00:00Z' },
      ],
    },
    {
      ID: 'sk-3',
      Key: 'legacy-check',
      Display: '旧版巡检（已停用）',
      Description: '已下线的历史技能示例。',
      Example: '历史巡检',
      Status: 'DISABLED',
      Revision: 2,
      CurrentVersionID: 'ver-1',
      CurrentNumber: 1,
      DraftVersionID: 'ver-1',
      DraftNumber: 1,
      HasDraft: false,
      Tags: [],
      ScopeIDs: [],
      ScopeNames: [],
      UpdatedAt: '2026-11-10T09:00:00Z',
      Versions: [
        { VersionID: 'ver-1', VersionNumber: 1, CreatedAt: '2026-11-10T09:00:00Z' },
      ],
    },
  ]
  const kbs = [
    {
      knowledge_base_id: 'guide',
      name: '操作手册',
      description: '值班与运维标准操作规范',
      revision: 1,
      document_count: 2,
      ready_count: 1,
      group_count: 1,
      updated_at: '2026-11-24T08:00:00Z',
    },
    {
      knowledge_base_id: 'faq',
      name: '故障排查库',
      description: '常见现场问题定位指南',
      revision: 1,
      document_count: 0,
      ready_count: 0,
      group_count: 0,
      updated_at: '2026-11-20T08:00:00Z',
    },
  ]
  const docsByKb = {
    guide: [
      {
        document_id: 'doc-1',
        knowledge_base_id: 'guide',
        filename: '值班手册.pdf',
        byte_size: 245760,
        status: 'READY',
        error_code: '',
        warning: '',
        revision: 2,
        published_revision: 2,
        updated_at: '2026-11-24T08:00:00Z',
      },
      {
        document_id: 'doc-2',
        knowledge_base_id: 'guide',
        filename: '应急预案.docx',
        byte_size: 98304,
        status: 'FAILED',
        error_code: 'DOCX_PARSE_ERROR',
        warning: '',
        revision: 1,
        published_revision: 0,
        updated_at: '2026-11-23T18:00:00Z',
      },
    ],
    faq: [],
  }

  function buildView() {
    return {
      ConfigPath: 'hivebridge.json',
      Root: 'D:\\work\\business-panels',
      Stamp: 'demo',
      Panels: panels,
      Environments: envList.map((e) => ({
        ...e,
        LoggedIn: Boolean(savedCreds[e.Name]),
        Account: savedCreds[e.Name]?.Name || '',
      })),
    }
  }
  let checked = []
  window.testCalls = {
    publish: 0,
    cancel: 0,
    login: 0,
    logout: 0,
    downloadSkill: 0,
    publishDraft: 0,
    downloadDoc: 0,
    retryDoc: 0,
    batchUpload: 0,
  }
  function rows(targets, status) {
    return targets.map((t) => {
      const p = panels.find((p) => p.Alias === t.Alias)
      return {
        ...t,
        Name: p.Name,
        Binding: p.Bindings[t.Env],
        Source: p.Source,
        Version: t.Env === '开发环境' ? 4 : 2,
        Bytes: 3670016,
        Status: status,
        Detail: '',
        URL: '',
      }
    })
  }
  window.runtime = { EventsOn() {}, BrowserOpenURL() {} }
  window.go = {
    main: {
      App: {
        async Load() {
          return buildView()
        },
        async Login(stamp, env, cred) {
          if (cred.Password !== 'demo' || !cred.Name.trim())
            throw Error('演示账号不匹配')
          savedCreds[env] = { Name: cred.Name.trim(), Password: cred.Password }
          window.testCalls.login++
        },
        async Logout(stamp, env) {
          delete savedCreds[env]
          window.testCalls.logout++
        },
        async SaveEnvironment(stamp, prev, input) {
          if (input.Login && input.Password !== 'demo')
            throw Error('演示账号不匹配')
          if (!prev) {
            envList.push({
              Name: input.Name,
              URL: input.URL,
              Tag: input.Tag || '',
            })
          } else {
            const idx = envList.findIndex((e) => e.Name === prev)
            if (idx >= 0)
              envList[idx] = {
                Name: input.Name,
                URL: input.URL,
                Tag: input.Tag || '',
              }
          }
          if (input.Login && input.Account) {
            savedCreds[input.Name] = {
              Name: input.Account,
              Password: input.Password,
            }
            window.testCalls.login++
          }
        },
        async DeleteEnvironment(stamp, name) {
          const idx = envList.findIndex((e) => e.Name === name)
          if (idx >= 0) envList.splice(idx, 1)
          delete savedCreds[name]
        },
        async ReorderEnvironments(stamp, names) {
          const byName = new Map(envList.map((e) => [e.Name, e]))
          envList.length = 0
          names.forEach((name, idx) => {
            const item = byName.get(name)
            if (item) envList.push({ ...item, Order: idx + 1 })
          })
        },
        async ListRemotePanels() {
          return [
            {
              SystemKey: 'ops',
              SystemName: '运维系统',
              ResourceKey: 'remote-new',
              DisplayName: '远端新面板',
              Binding: 'ops/remote-new',
              Version: 3,
            },
          ]
        },
        async SyncRemotePanels(stamp, env) {
          if (panels.some((p) => p.Alias === 'remote-new')) return 0
          panels.push({
            Alias: 'remote-new',
            Name: '远端新面板',
            Source: '',
            Bindings: { [env]: 'ops/remote-new' },
          })
          return 1
        },
        async SavePanel(stamp, prev, env, input) {
          const alias = prev || input.Binding.split('/')[1] || 'custom-panel'
          const existing = panels.find((p) => p.Alias === alias)
          if (existing) {
            existing.Name = input.Name
            existing.Source = input.Source
            if (input.Binding) existing.Bindings[env] = input.Binding
            else delete existing.Bindings[env]
          } else {
            panels.push({
              Alias: alias,
              Name: input.Name,
              Source: input.Source,
              Bindings: input.Binding ? { [env]: input.Binding } : {},
            })
          }
        },
        async DeletePanel(stamp, alias) {
          const idx = panels.findIndex((p) => p.Alias === alias)
          if (idx >= 0) panels.splice(idx, 1)
        },
        async ExportConfig() {
          return '/tmp/hivebridge.json'
        },
        async Preview(stamp, targets) {
          return { ID: '', Results: rows(targets, '打包通过') }
        },
        async Check(stamp, targets, credentials) {
          for (const t of targets) {
            const c = credentials?.[t.Env]?.Password
              ? credentials[t.Env]
              : savedCreds[t.Env]
            if (!c || c.Password !== 'demo')
              throw Error('环境 ' + t.Env + ' 尚未登录')
          }
          checked = targets.map(({ Env, Alias }) => ({ Env, Alias }))
          return { ID: 'checked', Results: rows(checked, '就绪') }
        },
        async Publish(id) {
          if (id !== 'checked') throw Error('检查过期')
          window.testCalls.publish++
          return { ID: '', Results: rows(checked, '已发布') }
        },
        async ChooseUpload() {
          return { Name: 'SKILL.md', Bytes: 2048 }
        },
        async ChooseSkillDir() {
          return { Name: 'new-patrol.zip', Bytes: 4096 }
        },
        async InspectUpload(stamp, kind, env) {
          if (!savedCreds[env]) throw Error('请先登录')
          return {
            ID: 'upload-skill',
            Kind: 'skill',
            Env: env,
            Key: 'new-patrol',
            Display: '巡检汇总',
            Example: '生成今日巡检汇总',
            Target: '',
            TargetID: '',
            Existing: false,
            Revision: 0,
            File: { Name: 'SKILL.md', Bytes: 2048 },
            Options: [{ ID: 'ops', Name: '运维系统' }],
          }
        },
        async CommitUpload(id, opts) {
          skills.unshift({
            ID: 'sk-new',
            Key: 'new-patrol',
            Display: opts.Display,
            Description: '新上传的巡检技能',
            Example: opts.Example,
            Status: 'ACTIVE',
            Revision: 1,
            CurrentVersionID: opts.Publish ? 'v1' : '',
            CurrentNumber: opts.Publish ? 1 : 0,
            DraftVersionID: 'v1',
            DraftNumber: 1,
            HasDraft: !opts.Publish,
            Tags: [],
            ScopeIDs: opts.Scope === '*' ? [] : [opts.Scope],
            ScopeNames: opts.Scope === '*' ? [] : ['运维系统'],
            UpdatedAt: '2026-11-24T11:30:00Z',
            Versions: [
              {
                VersionID: 'v1',
                VersionNumber: 1,
                CreatedAt: '2026-11-24T11:30:00Z',
              },
            ],
          })
          return {
            Status: opts.Publish ? '已发布' : '已保存草稿',
            Detail: 'new-patrol · 草稿 v1',
            Warning: '',
          }
        },
        async ListSkills(env) {
          return {
            Env: env,
            URL: 'https://dev.example.com',
            Skills: skills.map(({ Versions, ...rest }) => rest),
            Systems: [{ ID: 'ops', Name: '运维系统' }],
          }
        },
        async GetSkillDetail(env, id) {
          return skills.find((s) => s.ID === id)
        },
        async DownloadSkillVersion(env, id, ver, name) {
          window.testCalls.downloadSkill++
          return '已保存到 ' + name + '（2 个文件）'
        },
        async PublishSkillDraft(env, id) {
          const s = skills.find((x) => x.ID === id)
          if (s) {
            s.CurrentVersionID = s.DraftVersionID
            s.CurrentNumber = s.DraftNumber
            s.HasDraft = false
            s.Revision++
          }
          window.testCalls.publishDraft++
        },
        async SetSkillStatus(env, id, rev, enable) {
          const s = skills.find((x) => x.ID === id)
          if (s) {
            s.Status = enable ? 'ACTIVE' : 'DISABLED'
            s.Revision++
          }
        },
        async UpdateSkillMetadata(env, id, input) {
          const s = skills.find((x) => x.ID === id)
          if (s) {
            s.Display = input.Display
            s.Example = input.Example
            s.Revision++
          }
        },
        async ListKnowledgeBases() {
          return kbs
        },
        async SaveKnowledgeBase(env, id, name, description) {
          if (!id) {
            const newId = 'kb-' + (kbs.length + 1)
            kbs.push({
              knowledge_base_id: newId,
              name,
              description,
              revision: 1,
              document_count: 0,
              ready_count: 0,
              group_count: 0,
              updated_at: '2026-11-24T11:00:00Z',
            })
            docsByKb[newId] = []
          }
        },
        async DeleteKnowledgeBase(env, id) {
          const idx = kbs.findIndex((k) => k.knowledge_base_id === id)
          if (idx >= 0) kbs.splice(idx, 1)
        },
        async ListKnowledgeDocuments(env, baseId) {
          return docsByKb[baseId] || []
        },
        async DownloadKnowledgeDocument(env, baseId, docId, filename) {
          window.testCalls.downloadDoc++
          return '已保存到 ' + filename + '（245760 字节）'
        },
        async RetryKnowledgeDocument(env, baseId, docId) {
          window.testCalls.retryDoc++
          const d = (docsByKb[baseId] || []).find((x) => x.document_id === docId)
          if (d) {
            d.status = 'READY'
            d.error_code = ''
          }
          return d
        },
        async DeleteKnowledgeDocument(env, baseId, docId) {
          docsByKb[baseId] = (docsByKb[baseId] || []).filter(
            (x) => x.document_id !== docId,
          )
        },
        async ChooseKnowledgeFiles() {
          return [
            { Filename: '值班手册.pdf', ContentBase64: 'ZGVtbw==' },
            { Filename: '新入职指南.md', ContentBase64: 'ZGVtbw==' },
          ]
        },
        async UploadKnowledgeBatch(env, baseId, files) {
          window.testCalls.batchUpload++
          return files.map((f) => {
            const existing = (docsByKb[baseId] || []).find(
              (d) => d.filename === f.Filename,
            )
            if (existing) {
              existing.revision++
              return {
                Filename: f.Filename,
                Status: '可检索',
                Detail: '修订 r' + existing.revision + ' 已处理完成',
                Replaced: true,
                Revision: existing.revision,
              }
            }
            const nextDoc = {
              document_id: 'doc-new',
              knowledge_base_id: baseId,
              filename: f.Filename,
              byte_size: 4096,
              status: 'READY',
              error_code: '',
              warning: '',
              revision: 1,
              published_revision: 1,
              updated_at: '2026-11-24T11:30:00Z',
            }
            docsByKb[baseId].push(nextDoc)
            return {
              Filename: f.Filename,
              Status: '可检索',
              Detail: '修订 r1 已处理完成',
              Replaced: false,
              Revision: 1,
            }
          })
        },
        async Cancel() {
          window.testCalls.cancel++
        },
      },
    },
  }
}

// TRACE-REQ: DOM-140
test('首次前置登录引导、界面添加环境、面板发布、Skill与知识库双向管理', async () => {
  const server = await preview({
    preview: { host: '127.0.0.1', port: 0 },
    logLevel: 'error',
  })
  let browser
  try {
    browser = await (
      process.env.HIVEBRIDGE_WEBKIT === '1' ? webkit : chromium
    ).launch()
    const page = await browser.newPage({
      viewport: { width: 1120, height: 768 },
      deviceScaleFactor: 2,
    })
    const errors = []
    page.on('pageerror', (err) => errors.push(err.message))
    await page.addInitScript(installFixture)
    await page.goto('http://127.0.0.1:' + server.httpServer.address().port)
    assert.equal(await page.title(), 'HiveBridge')

    const capture = async (name) => {
      if (process.env.HIVEBRIDGE_SCREENSHOTS !== '1') return
      const path = fileURLToPath(new URL('../../images/', import.meta.url))
      await mkdir(path, { recursive: true })
      await page.screenshot({ path: path + name + '.png', animations: 'disabled' })
    }

    // 1. 首次启动呈现前置登录引导页，点击 开发环境 完成登录进入工作区
    await page.getByText('选择已有环境登录').waitFor()
    await capture('desktop-onboarding')
    await page.getByRole('button', { name: '登录环境 开发环境' }).click()
    await page.getByLabel('管理员账号').fill('demo')
    await page.getByLabel('密码', { exact: true }).fill('demo')
    await page.getByRole('button', { name: '登录并记住' }).click()

    // 2. 业务面板单环境聚焦勾选、预览、免密检查与发布
    const panelBox = (name) =>
      page.getByRole('checkbox', { name: '选择面板 ' + name, exact: true })
    await page.getByRole('checkbox', { name: '全选当前环境面板' }).check()
    await page.getByRole('checkbox', { name: '全选当前环境面板' }).uncheck()
    for (const name of ['运行概览', '工单列表', '任务调度'])
      await panelBox(name).check()
    assert.equal(await page.locator('tbody tr.row-chosen').count(), 3)
    await capture('desktop-selection')

    await page.getByRole('button', { name: '本地打包预览' }).click()
    await page.getByText('打包通过', { exact: true }).first().waitFor()
    await page
      .getByRole('dialog')
      .getByRole('button', { name: '关闭', exact: true })
      .last()
      .click()

    await page.getByRole('button', { name: '检查发布目标' }).click()
    await page.getByRole('button', { name: '确认发布 3 个就绪目标' }).waitFor()
    await capture('desktop-confirm')
    await page.getByRole('button', { name: '确认发布 3 个就绪目标' }).click()
    await page.getByText('3 / 3 个目标已确认发布').waitFor()
    await page
      .getByRole('dialog')
      .getByRole('button', { name: '关闭', exact: true })
      .last()
      .click()

    // 2.5 同步远端面板 & 点击目标资源键编辑并删除测试面板
    await page.getByRole('button', { name: '同步远端' }).click()
    await page.getByText('远端新面板').waitFor()
    await page
      .getByRole('button', { name: '配置面板 远端新面板 的目标资源键' })
      .click()
    await page.getByLabel('开发环境 · 目标资源键').fill('ops/remote-edited')
    await page.getByRole('button', { name: '保存修改' }).click()
    await page.getByText('ops/remote-edited').waitFor()
    await page
      .getByRole('button', { name: '配置面板 远端新面板 的目标资源键' })
      .click()
    await page.getByRole('button', { name: '删除面板' }).click()

    // 3. 左侧下方环境列表顺序验证、直接切换 + 环境管理页排序调整/新增/删除
    const readSideEnvNames = () =>
      page.locator('.side-env-scroll .env-name').allTextContents()
    assert.deepEqual(await readSideEnvNames(), [
      '开发环境',
      '联调环境',
      '预发环境',
      '生产环境',
    ])
    await page.getByRole('button', { name: '切换到环境 联调环境' }).click()
    await page.getByRole('button', { name: '切换到环境 开发环境' }).click()
    await page.getByRole('button', { name: '环境管理' }).click()
    await page.getByRole('heading', { name: '环境管理' }).waitFor()
    await capture('desktop-environments')
    await page.getByRole('button', { name: '下移环境 联调环境' }).click()
    assert.deepEqual(await readSideEnvNames(), [
      '开发环境',
      '预发环境',
      '联调环境',
      '生产环境',
    ])
    await page.getByRole('button', { name: '上移环境 联调环境' }).click()
    assert.deepEqual(await readSideEnvNames(), [
      '开发环境',
      '联调环境',
      '预发环境',
      '生产环境',
    ])
    await page.getByRole('button', { name: '添加环境' }).first().click()
    await page.getByLabel('环境名称').fill('临时验证环境')
    await page.getByLabel('服务地址').fill('https://tmp.example.com')
    await page.getByLabel('管理员账号').fill('demo')
    await page.getByLabel('密码', { exact: true }).fill('demo')
    await page.getByRole('button', { name: '添加并登录' }).click()
    await page.getByRole('button', { name: '切换到环境 临时验证环境' }).waitFor()
    await page.getByRole('button', { name: '切换到环境 开发环境' }).first().click()
    await page.getByRole('button', { name: '环境管理' }).click()
    await page.getByRole('button', { name: '删除环境 临时验证环境' }).click()

    // 4. 企业 Skill 双向管理：先看到完整列表与详情 -> 下载 -> 发布草稿 -> 上传新 Skill
    await page
      .getByRole('navigation')
      .getByRole('button', { name: '企业Skill' })
      .click()
    await page.getByRole('heading', { name: '值班报告助手' }).waitFor()
    // 默认仅展示「有效」Skill，隐藏已停用项；切到「停用」可见已停用项
    assert.equal(await page.locator('.master-item').count(), 2)
    await capture('desktop-skill')
    await page.locator('.pane-filter button', { hasText: '停用' }).click()
    assert.equal(await page.locator('.master-item').count(), 1)
    await page.locator('.pane-filter button', { hasText: '有效' }).click()
    assert.equal(await page.locator('.master-item').count(), 2)
    await page.getByRole('button', { name: '下载原包' }).click()
    assert.equal(await page.evaluate(() => window.testCalls.downloadSkill), 1)
    await page.getByRole('button', { name: '发布草稿' }).click()
    assert.equal(await page.evaluate(() => window.testCalls.publishDraft), 1)
    await page.getByRole('button', { name: '上传目录' }).click()
    await page.getByLabel('适用业务系统').selectOption('ops')
    await page.getByRole('button', { name: '确认保存Skill草稿' }).click()
    await page.getByRole('heading', { name: '已保存草稿' }).waitFor()
    await page.getByRole('button', { name: '完成' }).click()

    // 5. 知识库双栏文档管理：先看库与文档列表 -> 下载 -> 失败重试 -> 批量上传（含同名替换提示）
    await page
      .getByRole('navigation')
      .getByRole('button', { name: '知识库文档' })
      .click()
    await page.getByText('值班手册.pdf').waitFor()
    await capture('desktop-knowledge')
    await page.getByRole('button', { name: '下载' }).first().click()
    assert.equal(await page.evaluate(() => window.testCalls.downloadDoc), 1)
    await page.getByRole('button', { name: '重试' }).click()
    assert.equal(await page.evaluate(() => window.testCalls.retryDoc), 1)
    await page.getByRole('button', { name: '上传文档' }).click()
    await page.getByText('替换同名 · 当前 r2').waitFor()
    await page.getByRole('button', { name: '确认上传 2 份文档' }).click()
    await page.getByRole('button', { name: '完成' }).click()
    assert.equal(await page.evaluate(() => window.testCalls.batchUpload), 1)

    assert.deepEqual(errors, [])
  } finally {
    await browser?.close()
    await new Promise((resolve) => server.httpServer.close(resolve))
  }
})
