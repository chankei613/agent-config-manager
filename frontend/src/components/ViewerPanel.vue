<script setup lang="ts">
import { useViewerStore } from '@/stores/viewer'
import { kindLabel } from '@/lib/api'

const viewer = useViewerStore()

function shortPath(path: string): string {
  return path.split('/').slice(-3).join('/')
}
</script>

<template>
  <div v-if="viewer.file || viewer.diff || viewer.loading || viewer.error" class="viewer-backdrop" @click.self="viewer.close()">
    <div class="viewer">
      <header class="viewer-head">
        <template v-if="viewer.file">
          <strong :title="viewer.file.path">{{ shortPath(viewer.file.path) }}</strong>
          <span class="meta">{{ kindLabel(viewer.file.kind) }}</span>
          <span class="meta">{{ viewer.file.lines }} 行</span>
        </template>
        <template v-else-if="viewer.diff">
          <strong>差分</strong>
          <span class="meta">
            +{{ viewer.diff.added }} / -{{ viewer.diff.deleted }}
          </span>
        </template>
        <button class="close" @click="viewer.close()">閉じる</button>
      </header>

      <p v-if="viewer.loading" class="loading">読み込み中…</p>
      <p v-else-if="viewer.error" class="error">{{ viewer.error }}</p>

      <!-- 1ファイル表示 -->
      <template v-else-if="viewer.file">
        <p v-if="viewer.file.masked" class="mask-note">
          APIキーなど秘密の値は <code>***</code> に伏せています（キー名は残しています）。
        </p>
        <p v-if="viewer.file.truncated" class="mask-note">
          大きいファイルなので途中まで表示しています。
        </p>
        <pre class="file-body">{{ viewer.file.text }}</pre>
      </template>

      <!-- 差分表示 -->
      <template v-else-if="viewer.diff">
        <p class="meta paths">
          <span :title="viewer.diff.left_path">− {{ shortPath(viewer.diff.left_path) }}</span><br />
          <span :title="viewer.diff.right_path">+ {{ shortPath(viewer.diff.right_path) }}</span>
        </p>
        <p v-if="viewer.diff.masked" class="mask-note">
          秘密の値は伏せた状態で比較しています。
        </p>
        <p v-if="viewer.diff.identical" class="verdict ok">
          マスク後の内容は同一です（違いは秘密の値だけでした）。
        </p>

        <div class="diff-body">
          <div v-for="(line, i) in viewer.diff.lines" :key="i" class="diff-line" :class="line.type">
            <span class="lineno">{{ line.left_no || '' }}</span>
            <span class="lineno">{{ line.right_no || '' }}</span>
            <span class="sign">{{ line.type === 'add' ? '+' : line.type === 'del' ? '−' : ' ' }}</span>
            <span class="diff-text">{{ line.text }}</span>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>
