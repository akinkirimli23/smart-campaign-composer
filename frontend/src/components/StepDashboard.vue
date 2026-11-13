<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { useCampaignStore } from '../stores/campaign'
import type { Channel, ChannelCounts, MessageStatus, Recipient } from '../api/types'
import { formatDateTime } from '../lib/time'
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

// --- recipients -----------------------------------------------------------------
const statusLabels = Object.fromEntries(rows.map((r) => [r.key, r.label])) as Record<MessageStatus, string>

/** Messages per status over both channels, for the filter buttons. */
const byStatus = computed(() => {
  const sum: Record<MessageStatus, number> = { pending: 0, sending: 0, sent: 0, failed: 0, skipped: 0, cancelled: 0 }
  for (const { id } of channels) {
    const c = counts(id)
    if (!c) continue
    for (const { key } of rows) sum[key] += c[key]
  }
  return sum
})

// The ones that need attention first, as in the list itself.
const filters: MessageStatus[] = ['failed', 'skipped', 'cancelled', 'pending', 'sending', 'sent']

/** What happened, in the marketer's words; the provider's own text is shown next to it. */
function note(r: Recipient): string {
  switch (r.status) {
    case 'sent':
      return r.sent_at ? formatDateTime(new Date(r.sent_at)) : ''
    case 'skipped':
      return 'Onaydan sonra izni ya da adresi kalktı'
    case 'failed':
      return 'Gönderilemedi'
    case 'cancelled':
      return 'Kampanya iptal edildi'
    case 'pending':
      return r.error ? 'Yeniden denenecek' : 'Sırada'
    default:
      return 'Gönderiliyor'
  }
}

// The list opens in a side panel: the page behind it stops scrolling, and
// focus moves into the panel so Escape and Tab work from the keyboard.
const closeButton = ref<HTMLButtonElement | null>(null)
watch(
  () => store.recipientsOpen,
  async (open) => {
    document.body.classList.toggle('no-scroll', open)
    if (open) {
      await nextTick()
      closeButton.value?.focus()
    }
  },
)
onUnmounted(() => {
  document.body.classList.remove('no-scroll')
  store.closeRecipients()
})

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
      <p class="eyebrow">Böl. 4 — Gönderim<template v-if="store.campaign"> · kampanya #{{ store.campaign.id }}</template></p>
      <h1>Canlı <em>akış.</em></h1>
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

    <div class="recipients-entry">
      <div>
        <h2>Bu mesajlar kimlere gitti?</h2>
        <p class="muted">Alıcıları tek tek gör: kime teslim edildi, kime gitmedi ve neden.</p>
      </div>
      <div class="row" style="flex-wrap: wrap">
        <button v-if="byStatus.failed + byStatus.skipped > 0" class="small" type="button" @click="store.openRecipients('failed')">
          Yalnızca sorunlular <span class="muted">· {{ byStatus.failed + byStatus.skipped }}</span>
        </button>
        <button class="primary" type="button" @click="store.openRecipients()">
          <AppIcon name="users" :size="18" /> Alıcıları gör
        </button>
      </div>
    </div>
  </template>

  <Teleport to="body">
    <div v-if="store.recipientsOpen" class="drawer-backdrop" @click.self="store.closeRecipients()">
      <aside class="drawer" role="dialog" aria-modal="true" aria-labelledby="recipients-title" @keydown.esc="store.closeRecipients()">
    <div class="card-head">
      <div>
        <p class="eyebrow">Kampanya #{{ store.campaign?.id }}</p>
        <h2 id="recipients-title" class="section-title">Alıcılar</h2>
      </div>
      <div class="row">
        <button class="small" type="button" :disabled="store.recipientsLoading" @click="store.loadRecipients()">
          {{ store.recipientsLoading ? 'Yükleniyor…' : 'Yenile' }}
        </button>
        <button ref="closeButton" class="small" type="button" @click="store.closeRecipients()">Kapat</button>
      </div>
    </div>

      <div class="filters" role="group" aria-label="Duruma göre süz">
        <button class="filter" :class="{ on: store.recipientsFilter === '' }" type="button" @click="store.filterRecipients('')">
          Hepsi <span>{{ totals.total }}</span>
        </button>
        <template v-for="f in filters" :key="f">
          <button
            v-if="byStatus[f] > 0 || store.recipientsFilter === f"
            class="filter"
            :class="{ on: store.recipientsFilter === f }"
            type="button"
            @click="store.filterRecipients(f)"
          >
            {{ statusLabels[f] }} <span>{{ byStatus[f] }}</span>
          </button>
        </template>
      </div>

      <p v-if="store.recipientsError" class="alert error" role="alert">{{ store.recipientsError }}</p>

      <div v-if="store.recipients.length" class="table-wrap">
        <table class="recipient-table">
          <thead>
            <tr>
              <th scope="col">Alıcı</th>
              <th scope="col">Kanal</th>
              <th scope="col">Durum</th>
              <th scope="col" class="num">Deneme</th>
              <th scope="col">Ne oldu</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="r in store.recipients" :key="r.user_id + r.channel">
              <td>
                <strong>{{ r.name }}</strong>
                <span class="sub">{{ r.address }}</span>
              </td>
              <td>
                {{ r.channel === 'sms' ? 'SMS' : 'Email' }}
                <span class="sub">{{ r.language.toUpperCase() }}</span>
              </td>
              <td><span class="badge" :class="'m-' + r.status">{{ statusLabels[r.status] }}</span></td>
              <td class="num">{{ r.attempts }}</td>
              <td>
                {{ note(r) }}
                <code v-if="r.error && r.status !== 'skipped'" class="sub">{{ r.error }}</code>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else-if="!store.recipientsLoading && !store.recipientsError" class="hint">Bu durumda alıcı yok.</p>

      <div v-if="store.recipients.length < store.recipientsTotal" class="more">
        <button type="button" :disabled="store.recipientsLoading" @click="store.loadRecipients(true)">
          Daha fazla göster <span class="muted">· {{ store.recipients.length }} / {{ store.recipientsTotal }}</span>
        </button>
      </div>
      <p class="hint tight" style="margin-top: 12px">Telefon ve email adresleri gizlenerek gösterilir. Liste, “Yenile” ile ya da gönderim bittiğinde güncellenir.</p>
      </aside>
    </div>
  </Teleport>
</template>
