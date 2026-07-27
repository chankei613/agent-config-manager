<script setup lang="ts">
import { useInventoryStore } from '@/stores/inventory'
import { kindLabel, isSensitive, api } from '@/lib/api'
import { useViewerStore } from '@/stores/viewer'

const inv = useInventoryStore()
const viewer = useViewerStore()

/** セルの中身を開く。複数あるときは先頭を開く（一覧はPhase後半で対応） */
async function openCell(kind: string, project: string) {
  const files = await api.files(kind, project)
  if (files.length > 0) viewer.openFile(files[0].path)
}

function projectLabel(project: string): string {
  return project === '' ? 'ユーザー全体' : project
}
</script>

<template>
  <section v-if="inv.matrix">
    <p class="note">
      どの設定がどこにあるかの一覧です。「複数」は同じ場所に内容の違うファイルが複数あることを示します。
    </p>

    <div class="table-scroll">
      <table class="matrix">
        <thead>
          <tr>
            <th class="corner">種別</th>
            <th v-for="project in inv.matrix.projects" :key="project">
              {{ projectLabel(project) }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="kind in inv.matrix.kinds" :key="kind">
            <th class="row-head">
              {{ kindLabel(kind) }}
              <span v-if="isSensitive(kind)" class="sensitive" title="機密情報を含みうる">要注意</span>
            </th>
            <td v-for="project in inv.matrix.projects" :key="project">
              <button
                v-if="inv.cellAt(kind, project)"
                class="cell-link"
                @click="openCell(kind, project)"
              >
                <span class="count">{{ inv.cellAt(kind, project)!.count }}</span>
                <span v-if="!inv.cellAt(kind, project)!.hash" class="mixed">複数</span>
              </button>
              <span v-else class="none">—</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
