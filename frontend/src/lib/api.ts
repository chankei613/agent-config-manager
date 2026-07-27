// バックエンドへの唯一の入口。
// Wails上ではバインディング（window.go.main.App）を、ブラウザ単体の開発時は
// cmd/acmserve のHTTP APIを使う。どちらでも同じ関数を呼べるようにしてある。

export type Scope = 'user' | 'project'

export type Kind =
  | 'claude_md'
  | 'settings'
  | 'settings_local'
  | 'mcp_config'
  | 'agent'
  | 'command'
  | 'skill'
  | 'other'

export interface Summary {
  total_files: number
  by_kind: Record<string, number>
  project_count: number
  /** ここへ書くと別の場所の実体が変わるファイル数 */
  via_symlink: number
  orphans: number
  diverged_kinds: string[]
}

export interface MatrixCell {
  kind: string
  project: string
  count: number
  /** 空でなければそのセルの内容は1種類に揃っている */
  hash: string
}

export interface Matrix {
  kinds: string[]
  projects: string[]
  cells: MatrixCell[]
}

export interface DriftGroup {
  hash: string
  count: number
  projects: string[]
  paths: string[]
}

export interface Drift {
  kind: string
  /** 比較単位（例: "CLAUDE.md", "agents/foo.md"） */
  identity: string
  groups: DriftGroup[]
  diverged: boolean
}

export interface ConfigFile {
  id: number
  scope: Scope
  kind: string
  project: string
  path: string
  rel_path: string
  size: number
  hash: string
  is_symlink: boolean
  symlink_target: string
  broken: boolean
  real_path: string
  via_symlink: boolean
}

export interface SyncStats {
  added: number
  updated: number
  removed: number
  total: number
}


export interface Snapshot {
  id: number
  label: string
  note: string
  file_count: number
  created_at: string
}

export interface SnapshotEntry {
  path: string
  kind: string
  project: string
  hash: string
  size: number
}

export type ChangeType = 'restore' | 'recreate' | 'unchanged' | 'extra'

export interface Change {
  type: ChangeType
  path: string
  kind: string
  project: string
  /** true なら、このパスへの書き込みは離れた場所の実体に届く */
  via_symlink: boolean
  real_path?: string
}

export interface RestoreResult {
  restored: number
  recreated: number
  skipped: number
  /** 復元直前の自動バックアップ。ここへ戻せば復元を取り消せる */
  backup_id: number
  errors: string[]
}

export interface ScanRoots {
  user: string
  projects: string[]
}

// Wailsランタイムが注入するバインディング
interface WailsBindings {
  GetSummary(): Promise<Summary>
  GetMatrix(): Promise<Matrix>
  GetDrift(onlyDiverged: boolean): Promise<Drift[]>
  GetOrphans(): Promise<ConfigFile[]>
  GetFiles(kind: string, project: string): Promise<ConfigFile[]>
  Rescan(): Promise<SyncStats>
  GetScanRoots(): Promise<ScanRoots>
  ListSnapshots(): Promise<Snapshot[]>
  CreateSnapshot(label: string, note: string): Promise<Snapshot>
  GetSnapshotEntries(id: number): Promise<SnapshotEntry[]>
  PlanRestore(id: number): Promise<Change[]>
  RestoreSnapshot(id: number): Promise<RestoreResult>
  DeleteSnapshot(id: number): Promise<void>
}

function bindings(): WailsBindings | null {
  const w = globalThis as unknown as { go?: { main?: { App?: WailsBindings } } }
  return w.go?.main?.App ?? null
}

/** Wails上で動いているか。UIの表示切り替えにも使う。 */
export function isDesktop(): boolean {
  return bindings() !== null
}

interface WailsRuntime {
  EventsOn(event: string, callback: (...data: unknown[]) => void): void
}

/**
 * 起動直後のスキャン完了を受け取る。
 * 初回スキャンは非同期なので、これを購読しないと「0件」の画面が出たまま更新されない。
 * 戻り値は購読できたかどうか（ブラウザ開発時は false）。
 */
export function onScanComplete(handler: () => void): boolean {
  const w = globalThis as unknown as { runtime?: WailsRuntime }
  if (!w.runtime?.EventsOn) return false

  w.runtime.EventsOn('scan:complete', handler)
  return true
}

async function http<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(`/api/v1${path}`, init)
  if (!resp.ok) {
    throw new Error((await resp.text()).trim() || resp.statusText)
  }
  return (await resp.json()) as T
}

export const api = {
  summary: (): Promise<Summary> => bindings()?.GetSummary() ?? http<Summary>('/summary'),

  matrix: (): Promise<Matrix> => bindings()?.GetMatrix() ?? http<Matrix>('/matrix'),

  drift: (onlyDiverged: boolean): Promise<Drift[]> =>
    bindings()?.GetDrift(onlyDiverged) ??
    http<Drift[]>(`/drift${onlyDiverged ? '?diverged=true' : ''}`),

  orphans: (): Promise<ConfigFile[]> => bindings()?.GetOrphans() ?? http<ConfigFile[]>('/orphans'),

  files: (kind = '', project = ''): Promise<ConfigFile[]> => {
    const bound = bindings()
    if (bound) return bound.GetFiles(kind, project)

    const params = new URLSearchParams()
    if (kind) params.set('kind', kind)
    if (project) params.set('project', project)
    const query = params.toString()
    return http<ConfigFile[]>(`/files${query ? `?${query}` : ''}`)
  },

  rescan: (): Promise<SyncStats> =>
    bindings()?.Rescan() ?? http<SyncStats>('/rescan', { method: 'POST' }),

  scanRoots: (): Promise<ScanRoots> =>
    bindings()?.GetScanRoots() ?? Promise.resolve({ user: '', projects: [] }),

  snapshots: (): Promise<Snapshot[]> =>
    bindings()?.ListSnapshots() ?? http<Snapshot[]>('/snapshots'),

  createSnapshot: (label: string, note = ''): Promise<Snapshot> =>
    bindings()?.CreateSnapshot(label, note) ??
    http<Snapshot>('/snapshots', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ label, note }),
    }),

  /** 復元で何が起きるかを、何も書き換えずに取得する */
  planRestore: (id: number): Promise<Change[]> =>
    bindings()?.PlanRestore(id) ?? http<Change[]>(`/snapshots/${id}/plan`),

  restoreSnapshot: (id: number): Promise<RestoreResult> =>
    bindings()?.RestoreSnapshot(id) ??
    http<RestoreResult>(`/snapshots/${id}/restore`, { method: 'POST' }),

  deleteSnapshot: (id: number): Promise<void> =>
    bindings()?.DeleteSnapshot(id) ?? http<void>(`/snapshots/${id}`, { method: 'DELETE' }),
}

/** 種別の日本語表示名 */
export const KIND_LABELS: Record<string, string> = {
  claude_md: 'プロジェクト指示',
  settings: '設定',
  settings_local: 'ローカル設定',
  mcp_config: 'MCPサーバー',
  agent: 'Worker定義',
  command: 'コマンド',
  skill: 'スキル',
  other: 'その他',
}

export function kindLabel(kind: string): string {
  return KIND_LABELS[kind] ?? kind
}

/** 機密が入りうる種別。UIで注意表示する。 */
export function isSensitive(kind: string): boolean {
  return kind === 'settings_local' || kind === 'mcp_config'
}
