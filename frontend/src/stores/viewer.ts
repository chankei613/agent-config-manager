import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, kindLabel } from '@/lib/api'
import type { ConfigFile, FileContent, FileDiff } from '@/lib/api'

/**
 * 設定ファイルの一覧・中身・差分を見るためのストア。
 * 機密種別（settings.local.json / MCP設定）はバックエンド側でマスクされて届く。
 */
export const useViewerStore = defineStore('viewer', () => {
  /** 一覧モード。Worker定義のように同じ種別が大量にある場合はここから選ぶ。 */
  const files = ref<ConfigFile[]>([])
  const listTitle = ref('')
  const filter = ref('')

  const file = ref<FileContent | null>(null)
  const diff = ref<FileDiff | null>(null)
  const loading = ref(false)
  const error = ref('')

  const isOpen = computed(
    () =>
      files.value.length > 0 ||
      file.value !== null ||
      diff.value !== null ||
      loading.value ||
      error.value !== '',
  )

  /** 一覧に戻れるか（1件だけ直接開いた場合は戻り先が無い） */
  const canGoBack = computed(() => files.value.length > 0 && file.value !== null)

  const filteredFiles = computed(() => {
    const q = filter.value.trim().toLowerCase()
    if (!q) return files.value
    return files.value.filter(
      (f) => f.rel_path.toLowerCase().includes(q) || f.path.toLowerCase().includes(q),
    )
  })

  /**
   * 種別×プロジェクトのファイル一覧を開く。
   * 1件だけならそのまま中身を出す（一段クリックを省く）。
   */
  async function openList(kind: string, project: string): Promise<void> {
    loading.value = true
    error.value = ''
    file.value = null
    diff.value = null
    filter.value = ''
    try {
      const rows = await api.files(kind, project)
      listTitle.value = `${kindLabel(kind)}（${project || 'ユーザー全体'}） ${rows.length}件`

      if (rows.length === 1) {
        files.value = []
        file.value = await api.content(rows[0].path)
        return
      }
      files.value = rows
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  async function openFile(path: string): Promise<void> {
    loading.value = true
    error.value = ''
    diff.value = null
    try {
      file.value = await api.content(path)
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
      file.value = null
    } finally {
      loading.value = false
    }
  }

  async function openDiff(left: string, right: string): Promise<void> {
    loading.value = true
    error.value = ''
    file.value = null
    files.value = []
    try {
      diff.value = await api.diffFiles(left, right)
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
      diff.value = null
    } finally {
      loading.value = false
    }
  }

  /** 中身から一覧へ戻る */
  function back(): void {
    file.value = null
  }

  function close(): void {
    files.value = []
    listTitle.value = ''
    filter.value = ''
    file.value = null
    diff.value = null
    error.value = ''
  }

  return {
    files,
    listTitle,
    filter,
    filteredFiles,
    file,
    diff,
    loading,
    error,
    isOpen,
    canGoBack,
    openList,
    openFile,
    openDiff,
    back,
    close,
  }
})
