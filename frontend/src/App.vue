<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { onScanComplete } from '@/lib/api'
import { useInventoryStore } from '@/stores/inventory'
import OverviewView from '@/pages/OverviewView.vue'
import MatrixView from '@/pages/MatrixView.vue'
import DriftView from '@/pages/DriftView.vue'
import OrphansView from '@/pages/OrphansView.vue'
import SnapshotsView from '@/pages/SnapshotsView.vue'
import ViewerPanel from '@/components/ViewerPanel.vue'

type Tab = 'overview' | 'matrix' | 'drift' | 'orphans' | 'snapshots'

const inv = useInventoryStore()
const tab = ref<Tab>('overview')

const tabs: { id: Tab; label: string }[] = [
  { id: 'overview', label: '概要' },
  { id: 'matrix', label: '一覧' },
  { id: 'drift', label: '乖離' },
  { id: 'orphans', label: 'リンク切れ' },
  { id: 'snapshots', label: 'バージョン' },
]

onMounted(() => {
  inv.loadAll()
  // 起動直後のスキャンはmountより後に終わることがあるので、完了したら読み直す
  onScanComplete(() => inv.loadAll())
})
</script>

<template>
  <div class="app">
    <header class="topbar">
      <div class="brand">
        <span class="title">Agent Config Manager</span>
        <span class="tagline">AIのdotfiles manager</span>
      </div>
      <div class="actions">
        <span v-if="inv.lastScan" class="meta">{{ inv.lastScan }}</span>
        <button :disabled="inv.scanning" @click="inv.rescan()">
          {{ inv.scanning ? 'スキャン中…' : '再スキャン' }}
        </button>
      </div>
    </header>

    <nav class="tabs">
      <button
        v-for="t in tabs"
        :key="t.id"
        :class="{ active: tab === t.id }"
        @click="tab = t.id"
      >
        {{ t.label }}
        <span v-if="t.id === 'orphans' && inv.orphans.length" class="pill">{{ inv.orphans.length }}</span>
        <span v-if="t.id === 'drift' && inv.divergedDrifts.length" class="pill">
          {{ inv.divergedDrifts.length }}
        </span>
      </button>
    </nav>

    <main class="main">
      <p v-if="inv.error" class="error">{{ inv.error }}</p>
      <p v-else-if="inv.loading" class="loading">読み込み中…</p>

      <template v-else>
        <OverviewView v-if="tab === 'overview'" />
        <MatrixView v-else-if="tab === 'matrix'" />
        <DriftView v-else-if="tab === 'drift'" />
        <OrphansView v-else-if="tab === 'orphans'" />
        <SnapshotsView v-else />
      </template>
    </main>

    <ViewerPanel />
  </div>
</template>
