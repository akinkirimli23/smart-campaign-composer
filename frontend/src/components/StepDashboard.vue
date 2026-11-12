<script setup lang="ts">
import { computed, ref } from 'vue'
import { useCampaignStore } from '../stores/campaign'
import type { Channel, ChannelCounts } from '../api/types'
import AppIcon from './AppIcon.vue'

const store = useCampaignStore()

const channels: { id: Channel; label: string }[] = [
  { id: 'sms', label: 'SMS' },
  { id: 'email', label: 'Email' },
]

const rows: { key: keyof Omit<ChannelCounts, 'total'>; label: string }[] = [
  { key: 'sent', label: 'Teslim edildi' },
  { key: 'sending', label: 'Gönderiliyor' },
  { key: 'pending', label: 'Bekliyor' },
  { key: 'failed', label: 'Başarısız' },
  { key: 'skipped', label: 'Atlandı' },
  { key: 'cancelled', label: 'İptal edildi' },
]

function counts(channel: Channel): ChannelCounts | undefined {
  return store.status?.channels[channel]
}

function percent(part: number, total: number): number {
  return total === 0 ? 0 : (part / total) * 100
}

const totals = computed(() => {
  const sum = { total: 0, sent: 0, failed: 0, skipped: 0, cancelled: 0, started: 0, waiting: 0 }
  for (const { id } of channels) {
    const c = counts(id)
    if (!c) continue
    sum.total += c.total
    sum.sent += c.sent
    sum.failed += c.failed
    sum.skipped += c.skipped
    sum.cancelled += c.cancelled
    sum.started += c.total - c.pending - c.cancelled
    sum.waiting += c.pending + c.sending
  }
  return sum
})

const cancelled = computed(() => Boolean(store.status?.cancelled_at))

/** The campaign is waiting for its start time: nothing has left the queue yet. */
const waiting = computed(() => {
  const s = store.status
  return s !== null && !s.done && !cancelled.value && totals.value.started === 0 && new Date(s.send_at).getTime() > Date.now()
})

const startsAt = computed(() =>
  store.status ? new Date(store.status.send_at).toLocaleString('tr-TR', { dateStyle: 'long', timeStyle: 'short' }) : '',
)

// Cancelling cannot be undone, so it takes two clicks: the first one only
// asks. An inline question instead of a browser dialog keeps the live numbers
// visible while deciding.
const confirming = ref(false)

async function cancel() {
  confirming.value = false
  await store.cancelCampaign()
}
</script>

<template>
  <button v-if="store.standalone" class="ghost back with-icon" type="button" @click="store.showCampaigns()">
    <AppIcon name="arrow-left" :size="16" /> Kampanyalar
  </button>
  <div class="page-head">
    <div>
      <h1>Gönderim <span v-if="store.campaign" class="muted">· kampanya #{{ store.campaign.id }}</span></h1>
      <p v-if="store.status?.brief" class="brief-quote">{{ store.status.brief }}</p>
      <p v-if="store.status?.languages && store.status.languages.length > 1" class="hint tight" style="margin-top: 8px">
        Metin dilleri: {{ store.status.languages.map((code) => code.toUpperCase()).join(' · ') }}. Her alıcı kendi dilindeki metni alır.
      </p>
    </div>
    <button class="primary" type="button" @click="store.reset()"><AppIcon name="plus" :size="18" /> Yeni kampanya</button>
  </div>

  <p v-if="store.statusError" class="alert error" role="alert">{{ store.statusError }}</p>

  <div v-if="cancelled" class="alert warn" role="status">
    <strong>Kampanya iptal edildi.</strong>
    {{ totals.cancelled }} mesaj gönderilmedi<template v-if="totals.sent">; iptalden önce {{ totals.sent }} mesaj teslim edilmişti</template>.
  </div>
  <p v-else-if="store.done" class="alert ok" role="status">
    Gönderim tamamlandı: {{ totals.sent }} / {{ totals.total }} mesaj teslim edildi<template v-if="totals.failed">, {{ totals.failed }} başarısız</template><template v-if="totals.skipped">, {{ totals.skipped }} atlandı</template>.
  </p>
  <div v-else-if="waiting" class="alert info" role="status">
    <div class="row">
      <span><strong>Zamanlandı.</strong> Gönderim {{ startsAt }} tarihinde başlayacak.</span>
      <button v-if="!confirming" class="danger small" type="button" :disabled="store.cancelling" @click="confirming = true">
        {{ store.cancelling ? 'İptal ediliyor…' : 'Kampanyayı iptal et' }}
      </button>
    </div>
    <div v-if="confirming" class="row confirm">
      <span>Kampanya iptal edilsin mi? Hiçbir mesaj gönderilmeyecek ve bu geri alınamaz.</span>
      <span class="row">
        <button class="small" type="button" @click="confirming = false">Vazgeç</button>
        <button class="danger small" type="button" @click="cancel">Evet, iptal et</button>
      </span>
    </div>
  </div>
  <div v-else-if="store.status" class="alert info" role="status">
    <div class="row">
      <span class="row"><span class="spinner" aria-hidden="true" /> Gönderiliyor… sayılar canlı güncellenir.</span>
      <button v-if="!confirming" class="danger small" type="button" :disabled="store.cancelling" @click="confirming = true">
        {{ store.cancelling ? 'Durduruluyor…' : 'Gönderimi durdur' }}
      </button>
    </div>
    <div v-if="confirming" class="row confirm">
      <span>Gönderim durdurulsun mu? Henüz gönderilmemiş mesajlar iptal edilir; gönderilenler geri alınamaz.</span>
      <span class="row">
        <button class="small" type="button" @click="confirming = false">Vazgeç</button>
        <button class="danger small" type="button" @click="cancel">Evet, durdur</button>
      </span>
    </div>
  </div>

  <p v-if="!store.status && !store.statusError" class="status" role="status">
    <span class="spinner" aria-hidden="true" /> Yükleniyor…
  </p>

  <template v-if="store.status">
    <dl class="tiles">
      <div class="tile">
        <dt>Toplam mesaj</dt>
        <dd>{{ totals.total }}</dd>
      </div>
      <div class="tile ok">
        <dt>Teslim edildi</dt>
        <dd>{{ totals.sent }}</dd>
      </div>
      <div class="tile">
        <dt>Sırada</dt>
        <dd>{{ totals.waiting }}</dd>
      </div>
      <div class="tile" :class="{ bad: totals.failed > 0 }">
        <dt>Başarısız</dt>
        <dd>{{ totals.failed }}</dd>
      </div>
    </dl>

    <div class="channels">
      <template v-for="ch in channels" :key="ch.id">
        <section v-if="counts(ch.id)" class="card channel" :class="ch.id">
          <div class="channel-head">
            <span class="icon"><AppIcon :name="ch.id" /></span>
            <div>
              <h2>{{ ch.label }}</h2>
              <p class="muted">{{ counts(ch.id)!.sent }} / {{ counts(ch.id)!.total }} teslim edildi</p>
            </div>
            <span class="percent">{{ Math.floor(percent(counts(ch.id)!.sent, counts(ch.id)!.total)) }}%</span>
          </div>
          <div
            class="bar"
            role="progressbar"
            :aria-label="ch.label + ' gönderim ilerlemesi'"
            :aria-valuenow="counts(ch.id)!.sent"
            aria-valuemin="0"
            :aria-valuemax="counts(ch.id)!.total"
          >
            <span class="sent" :style="{ width: percent(counts(ch.id)!.sent, counts(ch.id)!.total) + '%' }" />
            <span class="sending" :style="{ width: percent(counts(ch.id)!.sending, counts(ch.id)!.total) + '%' }" />
            <span class="failed" :style="{ width: percent(counts(ch.id)!.failed, counts(ch.id)!.total) + '%' }" />
            <span class="skipped" :style="{ width: percent(counts(ch.id)!.skipped, counts(ch.id)!.total) + '%' }" />
          </div>
          <dl class="legend">
            <template v-for="r in rows" :key="r.key">
              <!-- "Cancelled" only appears once something was cancelled. -->
              <div v-if="r.key !== 'cancelled' || counts(ch.id)!.cancelled > 0">
                <span class="swatch" :class="r.key" />
                <dt>{{ r.label }}</dt>
                <dd>{{ counts(ch.id)![r.key] }}</dd>
              </div>
            </template>
          </dl>
        </section>
      </template>
    </div>

    <p class="hint" style="margin-top: 16px">
      “Bekliyor” yeniden denenecek mesajları da içerir. “Atlandı”, onaydan sonra izni ya da adresi kalkan alıcılardır.
    </p>
  </template>
</template>
