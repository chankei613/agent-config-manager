import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '@/lib/api'
import type { Change, RestoreResult, Snapshot } from '@/lib/api'

export const useSnapshotsStore = defineStore('snapshots', () => {
  const snapshots = ref<Snapshot[]>([])
  const loading = ref(false)
  const error = ref('')

  /** 復元前に必ず見せる予定表。null なら未確認。 */
  const plan = ref<Change[] | null>(null)
  const planFor = ref<number | null>(null)
  const lastResult = ref<RestoreResult | null>(null)
  const busy = ref(false)

  /** 実際に書き換えが起きるものだけ（unchanged と extra を除く） */
  const planWrites = computed(
    () => plan.value?.filter((c) => c.type === 'restore' || c.type === 'recreate') ?? [],
  )

  /** 書き込みが実体に届くもの。UIで強く警告する。 */
  const planSymlinkWrites = computed(() => planWrites.value.filter((c) => c.via_symlink))

  async function fetchAll(): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      snapshots.value = await api.snapshots()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  async function create(label: string, note = ''): Promise<void> {
    error.value = ''
    try {
      await api.createSnapshot(label || new Date().toLocaleString('ja-JP'), note)
      await fetchAll()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  /** 復元の予定を取得する。この時点では何も書き換わらない。 */
  async function preparePlan(id: number): Promise<void> {
    error.value = ''
    plan.value = null
    planFor.value = null
    try {
      plan.value = await api.planRestore(id)
      planFor.value = id
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  function cancelPlan(): void {
    plan.value = null
    planFor.value = null
  }

  /** 予定を確認済みのスナップショットだけ復元できる */
  async function confirmRestore(): Promise<void> {
    if (planFor.value === null) return

    busy.value = true
    error.value = ''
    try {
      lastResult.value = await api.restoreSnapshot(planFor.value)
      cancelPlan()
      await fetchAll()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      busy.value = false
    }
  }

  async function remove(id: number): Promise<void> {
    error.value = ''
    try {
      await api.deleteSnapshot(id)
      await fetchAll()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  function dismissResult(): void {
    lastResult.value = null
  }

  return {
    snapshots,
    loading,
    error,
    plan,
    planFor,
    planWrites,
    planSymlinkWrites,
    lastResult,
    busy,
    fetchAll,
    create,
    preparePlan,
    cancelPlan,
    confirmRestore,
    remove,
    dismissResult,
  }
})
