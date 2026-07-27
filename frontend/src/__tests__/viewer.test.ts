import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useViewerStore } from '@/stores/viewer'
import type { ConfigFile, FileContent } from '@/lib/api'

function configFile(partial: Partial<ConfigFile> = {}): ConfigFile {
  return {
    id: 1,
    scope: 'user',
    kind: 'agent',
    project: '',
    path: '/Users/c/.claude/agents/a.md',
    rel_path: 'agents/a.md',
    size: 2048,
    hash: 'h',
    is_symlink: false,
    symlink_target: '',
    broken: false,
    real_path: '',
    via_symlink: false,
    ...partial,
  }
}

function content(partial: Partial<FileContent> = {}): FileContent {
  return {
    path: '/Users/c/.claude/agents/a.md',
    kind: 'agent',
    size: 10,
    text: 'body',
    masked: false,
    truncated: false,
    lines: 1,
    ...partial,
  }
}

function mockApi(overrides: Record<string, unknown> = {}) {
  return {
    files: vi.fn().mockResolvedValue([configFile()]),
    content: vi.fn().mockResolvedValue(content()),
    diffFiles: vi.fn().mockResolvedValue({
      left_path: '/a',
      right_path: '/b',
      lines: [],
      added: 1,
      deleted: 1,
      masked: false,
      identical: false,
    }),
    ...overrides,
  }
}

let currentApi = mockApi()

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      files: (k: string, p: string) => currentApi.files(k, p),
      content: (path: string) => currentApi.content(path),
      diffFiles: (l: string, r: string) => currentApi.diffFiles(l, r),
    },
  }
})

beforeEach(() => {
  setActivePinia(createPinia())
  currentApi = mockApi()
})

describe('viewer store', () => {
  // Worker定義153件のように大量にある場合は一覧から選べること
  it('shows a list when a cell holds many files', async () => {
    const many = Array.from({ length: 153 }, (_, i) =>
      configFile({ id: i, path: `/agents/w${i}.md`, rel_path: `agents/w${i}.md` }),
    )
    currentApi = mockApi({ files: vi.fn().mockResolvedValue(many) })

    const store = useViewerStore()
    await store.openList('agent', '')

    expect(store.files).toHaveLength(153)
    expect(store.listTitle).toContain('153件')
    // 一覧を出す段階では中身は読まない
    expect(currentApi.content).not.toHaveBeenCalled()
  })

  it('opens the content directly when only one file exists', async () => {
    const store = useViewerStore()
    await store.openList('claude_md', '')

    expect(store.files).toHaveLength(0)
    expect(store.file?.text).toBe('body')
    // 1件だけのときは戻り先が無い
    expect(store.canGoBack).toBe(false)
  })

  it('filters the list by name', async () => {
    currentApi = mockApi({
      files: vi.fn().mockResolvedValue([
        configFile({ rel_path: 'agents/engineering-backend-architect.md' }),
        configFile({ rel_path: 'agents/marketing-seo-specialist.md' }),
        configFile({ rel_path: 'agents/game-designer.md' }),
      ]),
    })

    const store = useViewerStore()
    await store.openList('agent', '')
    expect(store.filteredFiles).toHaveLength(3)

    store.filter = 'marketing'
    expect(store.filteredFiles).toHaveLength(1)
    expect(store.filteredFiles[0].rel_path).toContain('marketing')

    store.filter = 'そんなものはない'
    expect(store.filteredFiles).toHaveLength(0)
  })

  it('can go back to the list after opening a file', async () => {
    currentApi = mockApi({
      files: vi.fn().mockResolvedValue([configFile({ rel_path: 'a.md' }), configFile({ rel_path: 'b.md' })]),
    })

    const store = useViewerStore()
    await store.openList('agent', '')
    await store.openFile('/agents/w1.md')

    expect(store.canGoBack).toBe(true)
    store.back()
    expect(store.file).toBeNull()
    // 一覧は残っている
    expect(store.files).toHaveLength(2)
  })

  it('reports masked content so the UI can say so', async () => {
    currentApi = mockApi({ content: vi.fn().mockResolvedValue(content({ masked: true })) })

    const store = useViewerStore()
    await store.openFile('/x/settings.local.json')

    expect(store.file?.masked).toBe(true)
  })

  it('clears the list when a diff is opened', async () => {
    currentApi = mockApi({
      files: vi.fn().mockResolvedValue([configFile({ rel_path: 'a.md' }), configFile({ rel_path: 'b.md' })]),
    })

    const store = useViewerStore()
    await store.openList('agent', '')
    await store.openDiff('/a', '/b')

    expect(store.files).toHaveLength(0)
    expect(store.diff?.added).toBe(1)
  })

  it('closes everything', async () => {
    const store = useViewerStore()
    await store.openFile('/x')
    store.close()

    expect(store.isOpen).toBe(false)
    expect(store.file).toBeNull()
  })

  it('surfaces errors instead of throwing', async () => {
    currentApi = mockApi({ content: vi.fn().mockRejectedValue(new Error('インベントリに無いファイルです')) })

    const store = useViewerStore()
    await store.openFile('/nowhere')

    expect(store.error).toBe('インベントリに無いファイルです')
    expect(store.file).toBeNull()
  })
})
