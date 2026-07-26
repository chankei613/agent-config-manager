<script setup lang="ts">
import { ref } from 'vue'
import { useInventoryStore } from '@/stores/inventory'
import { kindLabel } from '@/lib/api'

const inv = useInventoryStore()
const showAll = ref(false)

function shortPath(path: string): string {
  const parts = path.split('/')
  return parts.slice(-3).join('/')
}
</script>

<template>
  <section>
    <p class="note">
      同じ設定が複数の場所にあり、内容がズレているものを検出します。
      比較するのは<strong>同じ役割の設定どうし</strong>だけで、
      別々のWorker定義のように内容が違って当然のファイルは対象外です。
    </p>

    <label class="toggle">
      <input v-model="showAll" type="checkbox" />
      揃っているものも表示する
    </label>

    <p v-if="inv.divergedDrifts.length === 0" class="verdict ok">
      内容の乖離はありません。複数箇所にある設定はすべて揃っています。
    </p>

    <div
      v-for="d in showAll ? inv.drift : inv.divergedDrifts"
      :key="`${d.kind}:${d.identity}`"
      class="drift"
      :class="{ diverged: d.diverged }"
    >
      <div class="drift-head">
        <strong>{{ d.identity }}</strong>
        <span class="meta">{{ kindLabel(d.kind) }}</span>
        <span v-if="d.diverged" class="badge warn">{{ d.groups.length }}通りに分裂</span>
        <span v-else class="badge ok">揃っている</span>
      </div>

      <div v-for="(group, index) in d.groups" :key="group.hash" class="group">
        <div class="group-head">
          <span v-if="d.diverged && index === 0" class="badge majority">多数派</span>
          <span class="meta">{{ group.count }}件</span>
          <span class="meta">{{ group.projects.map((p) => p || 'ユーザー全体').join(', ') }}</span>
        </div>
        <ul class="paths">
          <li v-for="path in group.paths" :key="path" :title="path">{{ shortPath(path) }}</li>
        </ul>
      </div>
    </div>
  </section>
</template>
