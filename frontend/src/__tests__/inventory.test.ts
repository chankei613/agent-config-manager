import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useInventoryStore } from '@/stores/inventory'
import { isSensitive, kindLabel } from '@/lib/api'
import type { Drift, Matrix, Summary } from '@/lib/api'

function summary(partial: Partial<Summary> = {}): Summary {
  return {
    total_files: 0,
    by_kind: {},
    project_count: 0,
    via_symlink: 0,
    orphans: 0,
    diverged_kinds: [],
    ...partial,
  }
}

function drift(partial: Partial<Drift> = {}): Drift {
  return { kind: 'claude_md', identity: 'CLAUDE.md', groups: [], diverged: false, ...partial }
}

function matrix(partial: Partial<Matrix> = {}): Matrix {
  return { kinds: [], projects: [], cells: [], ...partial }
}

/** api モジュールをまとめて差し替える */
function mockApi(overrides: Partial<Record<string, unknown>> = {}) {
  const base = {
    summary: vi.fn().mockResolvedValue(summary()),
    matrix: vi.fn().mockResolvedValue(matrix()),
    drift: vi.fn().mockResolvedValue([]),
    orphans: vi.fn().mockResolvedValue([]),
    scanRoots: vi.fn().mockResolvedValue({ user: '/home/.claude', projects: [] }),
    rescan: vi.fn().mockResolvedValue({ added: 1, updated: 2, removed: 0, total: 3 }),
  }
  return { ...base, ...overrides }
}

// 呼び出し時に currentApi へ委譲する。こうしないとテストごとの差し替えが効かない。
vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      summary: () => currentApi.summary(),
      matrix: () => currentApi.matrix(),
      drift: (onlyDiverged: boolean) => currentApi.drift(onlyDiverged),
      orphans: () => currentApi.orphans(),
      scanRoots: () => currentApi.scanRoots(),
      rescan: () => currentApi.rescan(),
    },
  }
})

let currentApi = mockApi()

beforeEach(() => {
  setActivePinia(createPinia())
  currentApi = mockApi()
})

describe('inventory store', () => {
  it('loads everything in one pass', async () => {
    currentApi = mockApi({
      summary: vi.fn().mockResolvedValue(summary({ total_files: 159, project_count: 3 })),
    })

    const store = useInventoryStore()
    await store.loadAll()

    expect(store.summary?.total_files).toBe(159)
    expect(store.roots.user).toBe('/home/.claude')
    expect(store.error).toBe('')
    expect(store.loading).toBe(false)
  })

  it('reports zero issues when config is healthy', async () => {
    const store = useInventoryStore()
    await store.loadAll()

    expect(store.issueCount).toBe(0)
  })

  it('counts orphans and diverged kinds as issues', async () => {
    currentApi = mockApi({
      summary: vi
        .fn()
        .mockResolvedValue(summary({ orphans: 2, diverged_kinds: ['claude_md', 'settings'] })),
    })

    const store = useInventoryStore()
    await store.loadAll()

    expect(store.issueCount).toBe(4)
  })

  it('exposes only diverged drifts separately', async () => {
    currentApi = mockApi({
      drift: vi.fn().mockResolvedValue([
        drift({ identity: 'CLAUDE.md', diverged: true }),
        drift({ identity: 'settings.json', diverged: false }),
      ]),
    })

    const store = useInventoryStore()
    await store.loadAll()

    expect(store.drift).toHaveLength(2)
    expect(store.divergedDrifts).toHaveLength(1)
    expect(store.divergedDrifts[0].identity).toBe('CLAUDE.md')
  })

  it('surfaces errors instead of throwing', async () => {
    currentApi = mockApi({ summary: vi.fn().mockRejectedValue(new Error('backend down')) })

    const store = useInventoryStore()
    await store.loadAll()

    expect(store.error).toBe('backend down')
    expect(store.loading).toBe(false)
  })

  it('reports scan results after a rescan', async () => {
    const store = useInventoryStore()
    await store.rescan()

    expect(store.lastScan).toContain('3件')
    expect(store.lastScan).toContain('追加 1')
    expect(store.scanning).toBe(false)
  })

  it('looks up matrix cells by kind and project', async () => {
    currentApi = mockApi({
      matrix: vi.fn().mockResolvedValue(
        matrix({
          kinds: ['claude_md'],
          projects: ['', 'proj-a'],
          cells: [
            { kind: 'claude_md', project: '', count: 1, hash: 'abc' },
            { kind: 'claude_md', project: 'proj-a', count: 2, hash: '' },
          ],
        }),
      ),
    })

    const store = useInventoryStore()
    await store.loadAll()

    expect(store.cellAt('claude_md', 'proj-a')?.count).toBe(2)
    // hash が空 = そのセルに内容の違うファイルが複数ある
    expect(store.cellAt('claude_md', 'proj-a')?.hash).toBe('')
    expect(store.cellAt('claude_md', 'missing')).toBeNull()
  })
})

describe('表示ヘルパー', () => {
  it('種別を日本語で表示する', () => {
    expect(kindLabel('claude_md')).toBe('プロジェクト指示')
    expect(kindLabel('agent')).toBe('Worker定義')
    // 未知の種別はそのまま返す
    expect(kindLabel('unknown_kind')).toBe('unknown_kind')
  })

  it('機密を含みうる種別を判別する', () => {
    expect(isSensitive('settings_local')).toBe(true)
    expect(isSensitive('mcp_config')).toBe(true)
    expect(isSensitive('claude_md')).toBe(false)
  })
})
