<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useCampaignStore } from '../stores/campaign'
import type { CampaignState, CampaignSummary } from '../api/types'
import { formatDateTime } from '../lib/time'

const store = useCampaignStore()

const stateLabels: Record<CampaignState, string> = {
  scheduled: 'Zamanlandı',
  sending: 'Gönderiliyor',
  done: 'Tamamlandı',
  cancelled: 'İptal edildi',
}

function when(c: CampaignSummary): string {
  const at = formatDateTime(new Date(c.send_at))
  return c.state === 'scheduled' ? `${at} tarihinde başlayacak` : at
}

function progress(c: CampaignSummary): string {
  const m = c.messages
  const parts = [`${m.sent} / ${m.total} teslim edildi`]
  if (m.failed) parts.push(`${m.failed} başarısız`)
  if (m.cancelled) parts.push(`${m.cancelled} iptal`)
  return parts.join(' · ')
}

// The list is where scheduled and running campaigns are watched, so it keeps
// itself fresh while it is on screen.
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void store.loadCampaigns()
  timer = setInterval(() => void store.loadCampaigns(), 5000)
})
onUnmounted(() => clearInterval(timer))
</script>

<template>
  <section class="card">
    <div class="card-head">
      <div>
        <h2>Kampanyalar</h2>
        <p class="hint">Zamanlanmış, gönderilen ve tamamlanmış kampanyalar. Ayrıntı için birine tıkla.</p>
      </div>
      <button class="primary" type="button" @click="store.reset()">Yeni kampanya</button>
    </div>

    <p v-if="store.campaignsError" class="alert error" role="alert">{{ store.campaignsError }}</p>

    <p v-if="store.campaigns === null && !store.campaignsError" class="status" role="status">
      <span class="spinner" aria-hidden="true" /> Yükleniyor…
    </p>
    <p v-else-if="store.campaigns && store.campaigns.length === 0" class="hint">Henüz kampanya yok.</p>

    <ul v-else-if="store.campaigns" class="campaign-list">
      <li v-for="c in store.campaigns" :key="c.id">
        <button type="button" class="campaign-row" @click="store.openCampaign(c.id)">
          <span class="badge" :class="c.state">{{ stateLabels[c.state] }}</span>
          <span class="campaign-main">
            <span class="campaign-brief">{{ c.brief || 'Brief girilmemiş' }}</span>
            <span class="campaign-meta">#{{ c.id }} · {{ when(c) }}</span>
          </span>
          <span class="campaign-progress">{{ progress(c) }}</span>
        </button>
      </li>
    </ul>
  </section>
</template>
