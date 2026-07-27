import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '@/lib/api'
import type { Change, UnifyResult } from '@/lib/api'

/**
 * 乖離している設定を、選んだ内容に揃える。
 * 書き込みを伴うので、スナップショットの復元と同じく
 * 「予定を確認してから実行する」2段階にしてある。
 */
export const useUnifyStore = defineStore('unify', () => {
  const plan = ref<Change[] | null>(null)
  const source = ref<string>('')
  const lastResult = ref<UnifyResult | null>(null)
  const busy = ref(false)
  const error = ref('')

  /** 実際に書き換えが起きるものだけ */
  const writes = computed(
    () => plan.value?.filter((c) => c.type === 'restore' || c.type === 'recreate') ?? [],
  )

  /** 書き込みが離れた場所の実体に届くもの。強く警告する。 */
  const symlinkWrites = computed(() => writes.value.filter((c) => c.via_symlink))

  async function preparePlan(sourcePath: string): Promise<void> {
    error.value = ''
    plan.value = null
    source.value = ''
    try {
      plan.value = await api.planUnify(sourcePath)
      source.value = sourcePath
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  function cancel(): void {
    plan.value = null
    source.value = ''
  }

  /** 予定を確認済みのものだけ実行できる */
  async function confirm(): Promise<void> {
    if (!source.value) return

    busy.value = true
    error.value = ''
    try {
      lastResult.value = await api.unify(source.value)
      cancel()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      busy.value = false
    }
  }

  function dismissResult(): void {
    lastResult.value = null
  }

  return {
    plan,
    source,
    writes,
    symlinkWrites,
    lastResult,
    busy,
    error,
    preparePlan,
    cancel,
    confirm,
    dismissResult,
  }
})
