<script setup lang="ts">
import { useInventoryStore } from '@/stores/inventory'

const inv = useInventoryStore()
</script>

<template>
  <section>
    <p class="note">
      リンク先が見つからない設定です。エラーが出ないまま定義が読み込まれなくなるため、
      気付かないうちに機能が失われていることがあります。
    </p>

    <p v-if="inv.orphans.length === 0" class="verdict ok">リンク切れはありません。</p>

    <ul v-else class="orphan-list">
      <li v-for="orphan in inv.orphans" :key="orphan.path" class="orphan">
        <div class="orphan-path"><code>{{ orphan.path }}</code></div>
        <div class="orphan-target">
          リンク先: <code>{{ orphan.symlink_target }}</code>
        </div>
        <p class="hint">
          リンク先のパスが変わった可能性があります。次のコマンドで張り直せます。
        </p>
        <code class="fix">ln -sfn &lt;正しいパス&gt; {{ orphan.path }}</code>
      </li>
    </ul>
  </section>
</template>
