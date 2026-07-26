<script setup lang="ts">
import { useInventoryStore } from '@/stores/inventory'
import { kindLabel } from '@/lib/api'

const inv = useInventoryStore()
</script>

<template>
  <section v-if="inv.summary">
    <div class="stat-row">
      <div class="stat">
        <span class="stat-value">{{ inv.summary.total_files }}</span>
        <span class="stat-label">設定ファイル</span>
      </div>
      <div class="stat">
        <span class="stat-value">{{ inv.summary.project_count }}</span>
        <span class="stat-label">プロジェクト</span>
      </div>
      <div class="stat" :class="{ warn: inv.summary.orphans > 0 }">
        <span class="stat-value">{{ inv.summary.orphans }}</span>
        <span class="stat-label">リンク切れ</span>
      </div>
      <div class="stat" :class="{ warn: inv.summary.diverged_kinds.length > 0 }">
        <span class="stat-value">{{ inv.summary.diverged_kinds.length }}</span>
        <span class="stat-label">乖離</span>
      </div>
    </div>

    <!-- 0件を「健全」と表示してはいけない。読めていないだけの可能性が高い -->
    <div v-if="inv.summary.total_files === 0" class="verdict warn">
      <p>設定ファイルが1件も見つかりませんでした。</p>
      <p class="hint">
        スキャン対象が空か、macOSがフォルダへのアクセスを許可していない可能性があります。
        外部ボリューム上にある場合は、アクセス許可のダイアログで「許可」を選んでから再スキャンしてください。
      </p>
    </div>
    <p v-else-if="inv.issueCount === 0" class="verdict ok">
      設定は揃っています。リンク切れも内容の乖離もありません。
    </p>
    <p v-else class="verdict warn">対処が必要な項目が {{ inv.issueCount }} 件あります。</p>

    <h3>種別ごとの内訳</h3>
    <ul class="kind-list">
      <li v-for="(count, kind) in inv.summary.by_kind" :key="kind">
        <span class="kind-name">{{ kindLabel(String(kind)) }}</span>
        <span class="kind-count">{{ count }}</span>
      </li>
    </ul>

    <p v-if="inv.summary.via_symlink > 0" class="note">
      うち {{ inv.summary.via_symlink }} 件は親ディレクトリがシンボリックリンクです。
      このパスへ書き込むと、離れた場所にある実体が書き換わります。
    </p>

    <h3>スキャン対象</h3>
    <ul class="path-list">
      <li v-if="inv.roots.user"><code>{{ inv.roots.user }}</code></li>
      <li v-for="root in inv.roots.projects" :key="root"><code>{{ root }}</code></li>
      <li v-if="!inv.roots.user && inv.roots.projects.length === 0" class="empty">
        （ブラウザ開発モードのため未取得）
      </li>
    </ul>
  </section>
</template>
