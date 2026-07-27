import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSnapshotsStore } from '@/stores/snapshots'
import type { Change, Snapshot } from '@/lib/api'

function snapshot(partial: Partial<Snapshot> = {}): Snapshot {
  return { id: 1, label: 'v1', note: '', file_count: 3, created_at: '', ...partial }
}

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
    snapshots: vi.fn().mockResolvedValue([snapshot()]),
    createSnapshot: vi.fn().mockResolvedValue(snapshot()),
    planRestore: vi.fn().mockResolvedValue([]),
    restoreSnapshot: vi
      .fn()
      .mockResolvedValue({ restored: 1, recreated: 0, skipped: 2, backup_id: 9, errors: [] }),
    deleteSnapshot: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
}

let currentApi = mockApi()

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      snapshots: () => currentApi.snapshots(),
      createSnapshot: (l: string, n: string) => currentApi.createSnapshot(l, n),
      planRestore: (id: number) => currentApi.planRestore(id),
      restoreSnapshot: (id: number) => currentApi.restoreSnapshot(id),
      deleteSnapshot: (id: number) => currentApi.deleteSnapshot(id),
    },
  }
})

beforeEach(() => {
  setActivePinia(createPinia())
  currentApi = mockApi()
})

describe('snapshots store', () => {
  it('lists saved snapshots', async () => {
    const store = useSnapshotsStore()
    await store.fetchAll()

    expect(store.snapshots).toHaveLength(1)
    expect(store.error).toBe('')
  })

  // 復元前に必ず予定を確認させる。確認していないものは実行できない。
  it('refuses to restore without a reviewed plan', async () => {
    const store = useSnapshotsStore()
    await store.confirmRestore()

    expect(currentApi.restoreSnapshot).not.toHaveBeenCalled()
    expect(store.lastResult).toBeNull()
  })

  it('restores only after the plan is prepared', async () => {
    currentApi = mockApi({ planRestore: vi.fn().mockResolvedValue([change()]) })

    const store = useSnapshotsStore()
    await store.preparePlan(1)
    expect(store.planFor).toBe(1)
    // 予定を出した時点ではまだ書き換えていない
    expect(currentApi.restoreSnapshot).not.toHaveBeenCalled()

    await store.confirmRestore()
    expect(currentApi.restoreSnapshot).toHaveBeenCalledWith(1)
    expect(store.lastResult?.backup_id).toBe(9)
    // 実行後は予定をクリアして二度押しを防ぐ
    expect(store.plan).toBeNull()
    expect(store.planFor).toBeNull()
  })

  it('counts only files that actually get written', async () => {
    currentApi = mockApi({
      planRestore: vi.fn().mockResolvedValue([
        change({ type: 'restore', path: '/a' }),
        change({ type: 'recreate', path: '/b' }),
        change({ type: 'unchanged', path: '/c' }),
        change({ type: 'extra', path: '/d' }),
      ]),
    })

    const store = useSnapshotsStore()
    await store.preparePlan(1)

    // unchanged と extra は書き換えられないので含めない
    expect(store.planWrites.map((c) => c.path)).toEqual(['/a', '/b'])
  })

  // リンク経由の書き込みは実体に届くので、UIで別枠にして強く警告する
  it('separates writes that reach through a symlink', async () => {
    currentApi = mockApi({
      planRestore: vi.fn().mockResolvedValue([
        change({ path: '/normal', via_symlink: false }),
        change({ path: '/linked', via_symlink: true, real_path: '/real/linked' }),
        // unchanged はリンク経由でも書き換えないので数えない
        change({ path: '/skip', type: 'unchanged', via_symlink: true }),
      ]),
    })

    const store = useSnapshotsStore()
    await store.preparePlan(1)

    expect(store.planWrites).toHaveLength(2)
    expect(store.planSymlinkWrites.map((c) => c.path)).toEqual(['/linked'])
  })

  it('clears the plan when cancelled', async () => {
    currentApi = mockApi({ planRestore: vi.fn().mockResolvedValue([change()]) })

    const store = useSnapshotsStore()
    await store.preparePlan(1)
    store.cancelPlan()

    expect(store.plan).toBeNull()
    expect(store.planFor).toBeNull()
  })

  it('surfaces errors instead of throwing', async () => {
    currentApi = mockApi({ snapshots: vi.fn().mockRejectedValue(new Error('db locked')) })

    const store = useSnapshotsStore()
    await store.fetchAll()

    expect(store.error).toBe('db locked')
  })

  it('falls back to a timestamp when no label is given', async () => {
    const store = useSnapshotsStore()
    await store.create('')

    const [label] = currentApi.createSnapshot.mock.calls[0]
    expect(label).not.toBe('')
  })
})
