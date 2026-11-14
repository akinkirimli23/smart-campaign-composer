<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useCampaignStore } from '../stores/campaign'
import type { CampaignState, CampaignSummary } from '../api/types'
import { countByState, filterCampaigns } from '../lib/campaigns'
import { formatDateTime } from '../lib/time'
import AppIcon from './AppIcon.vue'

const store = useCampaignStore()

const stateLabels: Record<CampaignState, string> = {
  scheduled: 'Zamanlandı',
  sending: 'Gönderiliyor',
  done: 'Tamamlandı',
  cancelled: 'İptal edildi',
}

// Narrowing the list: by state and by text in the brief.
const states: CampaignState[] = ['scheduled', 'sending', 'done', 'cancelled']
const state = ref<CampaignState | ''>('')
const query = ref('')
const counts = computed(() => countByState(store.campaigns ?? []))
const shown = computed(() => filterCampaigns(store.campaigns ?? [], state.value, query.value))

function clearFilters() {
  state.value = ''
  query.value = ''
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

function percent(c: CampaignSummary): number {
  return c.messages.total ? (c.messages.sent / c.messages.total) * 100 : 0
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
  <div class="page-head">
    <div>
      <p class="eyebrow">Arşiv</p>
      <h1>Kampanyalar<em>.</em></h1>
      <p class="lead">Zamanlanmış, gönderilen ve tamamlanmış kampanyalar. Ayrıntı için birine tıkla.</p>
    </div>
    <button class="primary" type="button" @click="store.reset()"><AppIcon name="plus" :size="18" /> Yeni kampanya</button>
  </div>

  <p v-if="store.campaignsError" class="alert error" role="alert">{{ store.campaignsError }}</p>

  <p v-if="store.campaigns === null && !store.campaignsError" class="status" role="status">
    <span class="spinner" aria-hidden="true" /> Yükleniyor…
  </p>

  <section v-else-if="store.campaigns && store.campaigns.length === 0" class="card empty">
    <span class="icon"><AppIcon name="megaphone" :size="24" /></span>
    <strong>Henüz kampanya yok</strong>
    <p>İlk kampanyanı bir brief yazarak oluştur.</p>
    <button class="primary" type="button" @click="store.reset()">Kampanya oluştur</button>
  </section>

  <template v-else-if="store.campaigns">
    <div class="filter-bar">
      <input v-model="query" type="text" class="search" placeholder="Brief'te ara" aria-label="Kampanyalarda ara" />
    </div>
    <div class="filters" role="group" aria-label="Duruma göre süz">
      <button class="filter" :class="{ on: state === '' }" type="button" @click="state = ''">
        Hepsi <span>{{ store.campaigns.length }}</span>
      </button>
      <template v-for="s in states" :key="s">
        <button v-if="counts[s] > 0 || state === s" class="filter" :class="{ on: state === s }" type="button" @click="state = s">
          {{ stateLabels[s] }} <span>{{ counts[s] }}</span>
        </button>
      </template>
    </div>

    <section v-if="shown.length" class="card flush">
      <ul class="campaign-list">
        <li v-for="c in shown" :key="c.id">
          <button type="button" class="campaign-row" @click="store.openCampaign(c.id)">
            <span class="badge" :class="c.state">{{ stateLabels[c.state] }}</span>
            <span class="campaign-main">
              <span class="campaign-brief">{{ c.brief || 'Brief girilmemiş' }}</span>
              <span class="campaign-meta">#{{ c.id }} · {{ when(c) }}</span>
            </span>
            <span class="campaign-progress">
              <span class="meter" style="display: block"><span :style="{ width: percent(c) + '%' }" /></span>
              {{ progress(c) }}
            </span>
          </button>
        </li>
      </ul>
    </section>
    <p v-else class="hint">
      Bu süzgeçlere uyan kampanya yok.
      <button class="ghost small" type="button" @click="clearFilters">Süzgeçleri temizle</button>
    </p>
  </template>
</template>
