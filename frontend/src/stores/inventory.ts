import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '@/lib/api'
import type { ConfigFile, Drift, Matrix, ScanRoots, Summary } from '@/lib/api'

export const useInventoryStore = defineStore('inventory', () => {
  const summary = ref<Summary | null>(null)
  const matrix = ref<Matrix | null>(null)
  const drift = ref<Drift[]>([])
  const orphans = ref<ConfigFile[]>([])
  const roots = ref<ScanRoots>({ user: '', projects: [] })

  const loading = ref(false)
  const scanning = ref(false)
  const error = ref('')
  const lastScan = ref<string>('')

  /** 対処が必要なもの。ここが0なら設定は健全。 */
  const issueCount = computed(() => {
    if (!summary.value) return 0
    return summary.value.orphans + summary.value.diverged_kinds.length
  })

  const divergedDrifts = computed(() => drift.value.filter((d) => d.diverged))

  async function loadAll(): Promise<void> {
    loading.value = true
    error.value = ''
    try {
      const [s, m, d, o, r] = await Promise.all([
        api.summary(),
        api.matrix(),
        api.drift(false),
        api.orphans(),
        api.scanRoots(),
      ])
      summary.value = s
      matrix.value = m
      drift.value = d
      orphans.value = o
      roots.value = r
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  async function rescan(): Promise<void> {
    scanning.value = true
    error.value = ''
    try {
      const stats = await api.rescan()
      lastScan.value = `${stats.total}件（追加 ${stats.added} / 更新 ${stats.updated} / 削除 ${stats.removed}）`
      await loadAll()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      scanning.value = false
    }
  }

  /** マトリクスのセルを (kind, project) で引く */
  function cellAt(kind: string, project: string) {
    return matrix.value?.cells.find((c) => c.kind === kind && c.project === project) ?? null
  }

  return {
    summary,
    matrix,
    drift,
    orphans,
    roots,
    loading,
    scanning,
    error,
    lastScan,
    issueCount,
    divergedDrifts,
    loadAll,
    rescan,
    cellAt,
  }
})
