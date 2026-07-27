<script setup lang="ts">
import { ref } from 'vue'
import { useInventoryStore } from '@/stores/inventory'
import { useUnifyStore } from '@/stores/unify'
import { kindLabel } from '@/lib/api'

const inv = useInventoryStore()
const unify = useUnifyStore()
const showAll = ref(false)

function shortPath(path: string): string {
  const parts = path.split('/')
  return parts.slice(-3).join('/')
}

async function runUnify() {
  await unify.confirm()
  await inv.loadAll()
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
          <button
            v-if="d.diverged"
            class="unify-btn"
            @click="unify.preparePlan(group.paths[0])"
          >
            この内容に揃える
          </button>
        </div>
        <ul class="paths">
          <li v-for="path in group.paths" :key="path" :title="path">{{ shortPath(path) }}</li>
        </ul>
      </div>
    </div>

    <!-- 統一の予定（ドライラン）。確認してから実行する -->
    <div v-if="unify.plan" class="plan">
      <h3>この内容に揃えると次のようになります</h3>
      <p class="meta">元: {{ shortPath(unify.source) }}</p>

      <p v-if="unify.writes.length === 0" class="verdict ok">
        書き換えられるファイルはありません。すでに揃っています。
      </p>
      <template v-else>
        <p class="warn-line">{{ unify.writes.length }} 件のファイルが書き換えられます。</p>
        <p v-if="unify.symlinkWrites.length" class="error">
          うち {{ unify.symlinkWrites.length }} 件はシンボリックリンク経由です。
          <strong>離れた場所にある実体が書き換わります。</strong>
        </p>

        <ul class="change-list">
          <li v-for="c in unify.writes" :key="c.path" class="change">
            <span class="badge" :class="c.type">
              {{ c.type === 'restore' ? '上書き' : '作成' }}
            </span>
            <span class="meta">{{ c.project || 'ユーザー全体' }}</span>
            <span :title="c.path">{{ shortPath(c.path) }}</span>
          </li>
        </ul>
      </template>

      <div class="actions">
        <button :disabled="unify.busy" @click="runUnify()">
          {{ unify.busy ? '統一中…' : 'この内容で揃える' }}
        </button>
        <button @click="unify.cancel()">やめる</button>
      </div>
    </div>

    <div v-if="unify.lastResult" class="verdict ok result">
      <p>
        揃えました。書き換え {{ unify.lastResult.updated }} 件 /
        変更なし {{ unify.lastResult.skipped }} 件
      </p>
      <p class="hint">
        直前の状態は自動バックアップ #{{ unify.lastResult.backup_id }} に保存されています。
        「バージョン」タブから戻せます。
      </p>
      <button @click="unify.dismissResult()">閉じる</button>
    </div>

    <p v-if="unify.error" class="error">{{ unify.error }}</p>
  </section>
</template>
