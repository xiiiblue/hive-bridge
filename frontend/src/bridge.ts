export interface Panel {
  Alias: string
  Name: string
  Source: string
  Bindings: Record<string, string>
}
export interface PanelInput {
  Alias: string
  Name: string
  Source: string
  Binding: string
  ApplyAllEnvs?: boolean
}
export interface RemotePanelOption {
  SystemKey: string
  SystemName: string
  ResourceKey: string
  DisplayName: string
  Binding: string
  Version: number
}
export type EnvTag = '' | 'dev' | 'test' | 'prod'
export interface Env {
  Name: string
  URL: string
  Tag: EnvTag
  Order?: number
  LoggedIn: boolean
  Account: string
}
export interface EnvInput {
  Name: string
  URL: string
  Tag: EnvTag
  Order?: number
  Account: string
  Password: string
  Login: boolean
}
export interface View {
  ConfigPath: string
  Root: string
  Stamp: string
  Panels: Panel[]
  Environments: Env[]
}
export interface Target {
  Env: string
  Alias: string
}
export interface Credential {
  Name: string
  Password: string
}
export interface Result extends Target {
  Name: string
  Source: string
  Binding: string
  Status: string
  Detail: string
  URL: string
  Version: number
  Bytes: number
}
export interface Report {
  ID: string
  Results: Result[]
  Warning: string
}
export interface UploadFile {
  Name: string
  Bytes: number
}
export interface UploadOption {
  ID: string
  Name: string
}
export interface UploadReview {
  ID: string
  Env: string
  Kind: string
  Key: string
  Display: string
  Example: string
  Target: string
  TargetID: string
  File: UploadFile
  Options: UploadOption[]
  Existing: boolean
  Revision: number
}
export interface UploadReceipt {
  Status: string
  Detail: string
  Warning: string
}
export interface SkillItem {
  ID: string
  Key: string
  Display: string
  Description: string
  Example: string
  Status: string
  Revision: number
  CurrentVersionID: string
  CurrentNumber: number
  DraftVersionID: string
  DraftNumber: number
  HasDraft: boolean
  Tags: string[]
  ScopeIDs: string[]
  ScopeNames: string[]
  UpdatedAt: string
}
export interface SkillVersion {
  VersionID: string
  VersionNumber: number
  CreatedAt: string
}
export interface SkillDetail extends SkillItem {
  Versions: SkillVersion[]
}
export interface SkillCatalog {
  Env: string
  URL: string
  Skills: SkillItem[]
  Systems: UploadOption[]
}
export interface SkillMetadataInput {
  Display: string
  Example: string
  ScopeIDs: string[]
  Tags: string[]
  ExpectedRevision: number
}
export interface KnowledgeBaseItem {
  knowledge_base_id: string
  name: string
  description: string
  revision: number
  document_count: number
  ready_count: number
  group_count: number
  updated_at: string
}
export interface KnowledgeDocItem {
  document_id: string
  knowledge_base_id: string
  filename: string
  byte_size: number
  status: string
  error_code: string
  warning: string
  revision: number
  published_revision: number
  updated_at: string
}
export interface BatchUploadItem {
  Filename: string
  ContentBase64: string
}
export interface SkillDirEntry {
  Path: string
  ContentBase64: string
}
export interface BatchUploadResult {
  Filename: string
  Status: string
  Detail: string
  Replaced: boolean
  Revision: number
}

interface Bridge {
  Load(): Promise<View>
  Login(stamp: string, env: string, cred: Credential): Promise<void>
  Logout(stamp: string, env: string): Promise<void>
  SaveEnvironment(
    stamp: string,
    previousName: string,
    input: EnvInput,
  ): Promise<void>
  DeleteEnvironment(stamp: string, name: string): Promise<void>
  ReorderEnvironments(stamp: string, names: string[]): Promise<void>
  ListRemotePanels(env: string): Promise<RemotePanelOption[]>
  SyncRemotePanels(stamp: string, env: string): Promise<number>
  SavePanel(
    stamp: string,
    previousAlias: string,
    env: string,
    input: PanelInput,
  ): Promise<void>
  DeletePanel(stamp: string, alias: string): Promise<void>
  ChooseConfig(): Promise<void>
  ExportConfig(): Promise<string>
  ChooseRoot(): Promise<void>
  ChooseSource(stamp: string, alias: string): Promise<void>
  Preview(stamp: string, targets: Target[]): Promise<Report>
  Check(
    stamp: string,
    targets: Target[],
    creds: Record<string, Credential>,
  ): Promise<Report>
  Publish(id: string): Promise<Report>
  ChooseUpload(kind: string): Promise<UploadFile>
  ChooseSkillDir(): Promise<UploadFile>
  InspectUpload(
    stamp: string,
    kind: string,
    env: string,
    cred: Credential,
  ): Promise<UploadReview>
  InspectSkillUpload(
    stamp: string,
    env: string,
    filename: string,
    contentBase64: string,
  ): Promise<UploadReview>
  InspectSkillDirUpload(
    stamp: string,
    env: string,
    dirName: string,
    entries: SkillDirEntry[],
  ): Promise<UploadReview>
  Library(id: string, lib: string): Promise<UploadReview>
  CommitUpload(
    id: string,
    opts: { Scope: string; Display: string; Example: string; Publish: boolean },
  ): Promise<UploadReceipt>
  ListSkills(env: string): Promise<SkillCatalog>
  GetSkillDetail(env: string, resourceID: string): Promise<SkillDetail>
  DownloadSkillVersion(
    env: string,
    resourceID: string,
    versionID: string,
    defaultName: string,
  ): Promise<string>
  PublishSkillDraft(
    env: string,
    resourceID: string,
    expectedRevision: number,
  ): Promise<void>
  SetSkillStatus(
    env: string,
    resourceID: string,
    expectedRevision: number,
    enable: boolean,
  ): Promise<void>
  UpdateSkillMetadata(
    env: string,
    resourceID: string,
    input: SkillMetadataInput,
  ): Promise<void>
  ListKnowledgeBases(env: string): Promise<KnowledgeBaseItem[]>
  SaveKnowledgeBase(
    env: string,
    id: string,
    name: string,
    description: string,
    expectedRevision: number,
  ): Promise<void>
  DeleteKnowledgeBase(
    env: string,
    id: string,
    expectedRevision: number,
  ): Promise<void>
  ListKnowledgeDocuments(
    env: string,
    baseID: string,
  ): Promise<KnowledgeDocItem[]>
  DownloadKnowledgeDocument(
    env: string,
    baseID: string,
    docID: string,
    filename: string,
  ): Promise<string>
  RetryKnowledgeDocument(
    env: string,
    baseID: string,
    docID: string,
    expectedRevision: number,
  ): Promise<KnowledgeDocItem>
  DeleteKnowledgeDocument(
    env: string,
    baseID: string,
    docID: string,
    expectedRevision: number,
  ): Promise<void>
  ChooseKnowledgeFiles(): Promise<BatchUploadItem[]>
  UploadKnowledgeBatch(
    env: string,
    baseID: string,
    files: BatchUploadItem[],
  ): Promise<BatchUploadResult[]>
  Cancel(): Promise<void>
}
declare global {
  interface Window {
    go: { main: { App: Bridge } }
    runtime: {
      EventsOn(name: string, cb: (value: string) => void): () => void
      BrowserOpenURL(url: string): void
    }
  }
}
export const api = () => window.go.main.App

export function envTagLabel(tag: string) {
  return (
    { dev: '开发', test: '测试', prod: '正式' }[tag as 'dev' | 'test' | 'prod'] ||
    ''
  )
}

export async function fileToBase64(file: File): Promise<string> {
  const buf = await file.arrayBuffer()
  const bytes = new Uint8Array(buf)
  let binary = ''
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk))
  }
  return btoa(binary)
}
