import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '@/lib/api'
import type { FileContent, FileDiff } from '@/lib/api'

/**
 * 設定ファイルの中身と差分を見るためのストア。
 * 機密種別（settings.local.json / MCP設定）はバックエンド側でマスクされて届く。
 */
export const useViewerStore = defineStore('viewer', () => {
  const file = ref<FileContent | null>(null)
  const diff = ref<FileDiff | null>(null)
  const loading = ref(false)
  const error = ref('')

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
    try {
      diff.value = await api.diffFiles(left, right)
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
      diff.value = null
    } finally {
      loading.value = false
    }
  }

  function close(): void {
    file.value = null
    diff.value = null
    error.value = ''
  }

  return { file, diff, loading, error, openFile, openDiff, close }
})
