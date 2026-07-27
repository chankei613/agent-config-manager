import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useUnifyStore } from '@/stores/unify'
import type { Change } from '@/lib/api'

function change(partial: Partial<Change> = {}): Change {
  return {
    type: 'restore',
    path: '/p/CLAUDE.md',
    kind: 'claude_md',
    project: 'p',
    via_symlink: false,
    ...partial,
  }
}

function mockApi(overrides: Record<string, unknown> = {}) {
  return {
    planUnify: vi.fn().mockResolvedValue([change()]),
    unify: vi.fn().mockResolvedValue({ updated: 1, skipped: 0, backup_id: 7, errors: [] }),
    ...overrides,
  }
}

let currentApi = mockApi()

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      planUnify: (p: string) => currentApi.planUnify(p),
      unify: (p: string) => currentApi.unify(p),
    },
  }
})

beforeEach(() => {
  setActivePinia(createPinia())
  currentApi = mockApi()
})

describe('unify store', () => {
  // 予定を確認していないものは実行できない
  it('refuses to run without a reviewed plan', async () => {
    const store = useUnifyStore()
    await store.confirm()

    expect(currentApi.unify).not.toHaveBeenCalled()
    expect(store.lastResult).toBeNull()
  })

  it('runs only after the plan is prepared', async () => {
    const store = useUnifyStore()
    await store.preparePlan('/p/CLAUDE.md')

    // 予定を出しただけでは書き換えない
    expect(currentApi.unify).not.toHaveBeenCalled()
    expect(store.source).toBe('/p/CLAUDE.md')

    await store.confirm()
    expect(currentApi.unify).toHaveBeenCalledWith('/p/CLAUDE.md')
    expect(store.lastResult?.backup_id).toBe(7)
    // 二度押しを防ぐため予定はクリアする
    expect(store.plan).toBeNull()
  })

  it('counts only files that actually get written', async () => {
    currentApi = mockApi({
      planUnify: vi.fn().mockResolvedValue([
        change({ type: 'restore', path: '/a' }),
        change({ type: 'recreate', path: '/b' }),
        change({ type: 'unchanged', path: '/c' }),
      ]),
    })

    const store = useUnifyStore()
    await store.preparePlan('/src')

    expect(store.writes.map((c) => c.path)).toEqual(['/a', '/b'])
  })

  it('separates writes that reach through a symlink', async () => {
    currentApi = mockApi({
      planUnify: vi.fn().mockResolvedValue([
        change({ path: '/normal' }),
        change({ path: '/linked', via_symlink: true }),
      ]),
    })

    const store = useUnifyStore()
    await store.preparePlan('/src')

    expect(store.symlinkWrites.map((c) => c.path)).toEqual(['/linked'])
  })

  it('surfaces errors instead of throwing', async () => {
    currentApi = mockApi({ planUnify: vi.fn().mockRejectedValue(new Error('統一先がありません')) })

    const store = useUnifyStore()
    await store.preparePlan('/src')

    expect(store.error).toBe('統一先がありません')
    expect(store.plan).toBeNull()
  })
})
