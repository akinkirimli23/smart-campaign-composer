<script setup lang="ts">
import { computed } from 'vue'
import { useCampaignStore } from '../stores/campaign'
import type { Channel, ChannelCounts } from '../api/types'

const store = useCampaignStore()

const channels: { id: Channel; label: string }[] = [
  { id: 'sms', label: 'SMS' },
  { id: 'email', label: 'Email' },
]

function counts(channel: Channel): ChannelCounts | undefined {
  return store.status?.channels[channel]
}

function percent(part: number, total: number): number {
  return total === 0 ? 0 : (part / total) * 100
}

const totals = computed(() => {
  const sum = { total: 0, sent: 0, failed: 0, skipped: 0 }
  for (const { id } of channels) {
    const c = counts(id)
    if (!c) continue
    sum.total += c.total
    sum.sent += c.sent
    sum.failed += c.failed
    sum.skipped += c.skipped
  }
  return sum
})
</script>

<template>
  <section class="card">
    <h2>Gönderim <span v-if="store.campaign" class="muted">· kampanya #{{ store.campaign.id }}</span></h2>

    <p v-if="store.statusError" class="alert error" role="alert">{{ store.statusError }}</p>
    <p v-else-if="store.done" class="alert ok" role="status">
      Gönderim tamamlandı: {{ totals.sent }} / {{ totals.total }} mesaj teslim edildi<template v-if="totals.failed">, {{ totals.failed }} başarısız</template><template v-if="totals.skipped">, {{ totals.skipped }} atlandı</template>.
    </p>
    <p v-else class="status" role="status"><span class="spinner" aria-hidden="true" /> Gönderiliyor… sayılar canlı güncellenir.</p>

    <div v-for="ch in channels" :key="ch.id" class="channel">
      <template v-if="counts(ch.id)">
        <div class="row between">
          <h3>{{ ch.label }}</h3>
          <span class="counter">{{ counts(ch.id)!.sent }} / {{ counts(ch.id)!.total }}</span>
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
          <span class="failed" :style="{ width: percent(counts(ch.id)!.failed, counts(ch.id)!.total) + '%' }" />
          <span class="skipped" :style="{ width: percent(counts(ch.id)!.skipped, counts(ch.id)!.total) + '%' }" />
        </div>
        <dl class="stats small">
          <div><dt>Bekliyor</dt><dd>{{ counts(ch.id)!.pending }}</dd></div>
          <div><dt>Gönderiliyor</dt><dd>{{ counts(ch.id)!.sending }}</dd></div>
          <div><dt>Teslim edildi</dt><dd>{{ counts(ch.id)!.sent }}</dd></div>
          <div><dt>Başarısız</dt><dd>{{ counts(ch.id)!.failed }}</dd></div>
          <div><dt>Atlandı</dt><dd>{{ counts(ch.id)!.skipped }}</dd></div>
        </dl>
      </template>
    </div>

    <p class="hint">
      “Bekliyor” yeniden denenecek mesajları da içerir. “Atlandı”, onaydan sonra izni ya da adresi kalkan alıcılardır.
    </p>
    <div class="actions">
      <button class="primary" type="button" @click="store.reset()">Yeni kampanya</button>
    </div>
  </section>
</template>
