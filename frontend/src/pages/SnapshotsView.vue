<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useSnapshotsStore } from '@/stores/snapshots'
import { useInventoryStore } from '@/stores/inventory'
import { kindLabel } from '@/lib/api'

const snaps = useSnapshotsStore()
const inv = useInventoryStore()
const label = ref('')

onMounted(() => snaps.fetchAll())

async function save() {
  await snaps.create(label.value)
  label.value = ''
  await inv.loadAll()
}

async function restore() {
  await snaps.confirmRestore()
  await inv.loadAll()
}

function shortPath(path: string): string {
  return path.split('/').slice(-3).join('/')
}
</script>

<template>
  <section>
    <p class="note">
      現在の設定をまとめて1バージョンとして保存し、あとから戻せます。
      復元する前に必ず「何が起きるか」を確認できます。
    </p>

    <form class="create-form" @submit.prevent="save">
      <input v-model="label" placeholder="バージョン名（空欄なら日時）" aria-label="ラベル" />
      <button type="submit">現在の状態を保存</button>
    </form>

    <p v-if="snaps.error" class="error">{{ snaps.error }}</p>

    <!-- 復元結果 -->
    <div v-if="snaps.lastResult" class="verdict ok result">
      <p>
        復元しました。書き戻し {{ snaps.lastResult.restored }} 件 /
        作り直し {{ snaps.lastResult.recreated }} 件 /
        変更なし {{ snaps.lastResult.skipped }} 件
      </p>
      <p class="hint">
        直前の状態は自動バックアップ #{{ snaps.lastResult.backup_id }} に保存されています。
        戻したい場合はそれを復元してください。
      </p>
      <p v-for="e in snaps.lastResult.errors" :key="e" class="error small">{{ e }}</p>
      <button @click="snaps.dismissResult()">閉じる</button>
    </div>

    <!-- 復元の予定（ドライラン）。これを見せてから実行する -->
    <div v-if="snaps.plan" class="plan">
      <h3>復元すると次のようになります</h3>

      <p v-if="snaps.planWrites.length === 0" class="verdict ok">
        書き換えられるファイルはありません。現在の状態と同じです。
      </p>
      <template v-else>
        <p class="warn-line">{{ snaps.planWrites.length }} 件のファイルが書き換えられます。</p>

        <p v-if="snaps.planSymlinkWrites.length" class="error">
          うち {{ snaps.planSymlinkWrites.length }} 件はシンボリックリンク経由です。
          <strong>離れた場所にある実体が書き換わります。</strong>
        </p>

        <ul class="change-list">
          <li v-for="c in snaps.planWrites" :key="c.path" class="change">
            <span class="badge" :class="c.type">
              {{ c.type === 'restore' ? '書き戻し' : '作り直し' }}
            </span>
            <span class="meta">{{ kindLabel(c.kind) }}</span>
            <span :title="c.path">{{ shortPath(c.path) }}</span>
            <span v-if="c.via_symlink" class="badge link" :title="c.real_path">実体へ反映</span>
          </li>
        </ul>
      </template>

      <div class="actions">
        <button :disabled="snaps.busy" @click="restore()">
          {{ snaps.busy ? '復元中…' : 'この内容で復元する' }}
        </button>
        <button @click="snaps.cancelPlan()">やめる</button>
      </div>
    </div>

    <h3>保存済みバージョン</h3>
    <p v-if="snaps.loading">読み込み中…</p>
    <p v-else-if="snaps.snapshots.length === 0" class="empty">
      まだ保存されていません。設定を変更する前に保存しておくと安全です。
    </p>

    <ul v-else class="snapshot-list">
      <li v-for="s in snaps.snapshots" :key="s.id" class="snapshot">
        <div class="snapshot-head">
          <strong>{{ s.label || `#${s.id}` }}</strong>
          <span class="meta">{{ s.file_count }} ファイル</span>
        </div>
        <p v-if="s.note" class="meta">{{ s.note }}</p>
        <div class="actions">
          <button @click="snaps.preparePlan(s.id)">復元内容を確認</button>
          <button @click="snaps.remove(s.id)">削除</button>
        </div>
      </li>
    </ul>
  </section>
</template>
