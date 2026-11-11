<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useCampaignStore } from '../stores/campaign'
import ErrorBox from './ErrorBox.vue'

const store = useCampaignStore()
const segment = computed(() => store.segment)
const audience = computed(() => segment.value?.audience)

// Assumptions belong to the condition they explain.
function assumptionsFor(index: number) {
  return (segment.value?.assumptions ?? []).filter((a) => a.condition_index === index)
}

const ruleDraft = ref('')
watch(
  () => segment.value?.rule,
  (rule) => {
    if (rule) ruleDraft.value = JSON.stringify(rule, null, 2)
  },
  { immediate: true },
)

const reachable = computed(() => (audience.value ? audience.value.sms + audience.value.email > 0 : false))
</script>

<template>
  <section v-if="segment?.status === 'unsupported'" class="card">
    <h2>Bu brief için kitle kuralı çıkarılamadı</h2>
    <div class="alert warn" role="alert">
      <strong>{{ segment.unsupported?.reason }}</strong>
      <ul v-if="segment.unsupported?.phrases.length">
        <li v-for="p in segment.unsupported.phrases" :key="p">“{{ p }}”</li>
      </ul>
    </div>
    <p class="hint">Eksik koşullu bir kural daha geniş bir kitleye gönderim demek olurdu, bu yüzden kısmi bir kural önermiyoruz. Brief'i sadeleştirip yeniden deneyebilirsin.</p>
    <div class="actions">
      <button class="primary" type="button" @click="store.step = 'brief'">Brief'i düzenle</button>
    </div>
  </section>

  <section v-else-if="segment && audience" class="card">
    <h2>Kitle</h2>
    <p class="hint">Bu açıklama kuraldan türetilir; gönderimde kullanılacak sorgu tam olarak budur.</p>

    <p class="summary">{{ segment.description?.summary }}</p>
    <ul class="conditions">
      <li v-for="(line, i) in segment.description?.conditions ?? []" :key="i">
        {{ line }}
        <p v-for="a in assumptionsFor(i)" :key="a.phrase" class="assumption">
          <span class="tag">Yorum</span> “{{ a.phrase }}” → {{ a.interpretation }}
        </p>
      </li>
    </ul>

    <dl class="stats">
      <div>
        <dt>Kurala uyan</dt>
        <dd>{{ audience.total }}</dd>
      </div>
      <div>
        <dt>SMS gidecek</dt>
        <dd>{{ audience.sms }}</dd>
      </div>
      <div>
        <dt>Email gidecek</dt>
        <dd>{{ audience.email }}</dd>
      </div>
      <div class="muted">
        <dt>Ulaşılamayan</dt>
        <dd>{{ audience.unreachable }}</dd>
      </div>
    </dl>
    <p class="hint">Bir kanaldan gönderim için hem izin hem adres gerekir. Ulaşılamayanlar iki kanalda da bunlardan birine sahip değil.</p>

    <details class="advanced">
      <summary>Kuralı elle düzenle (JSON)</summary>
      <textarea v-model="ruleDraft" rows="12" spellcheck="false" class="mono" aria-label="Kural JSON" />
      <button type="button" :disabled="store.segmentLoading" @click="store.recountSegment(ruleDraft)">
        {{ store.segmentLoading ? 'Doğrulanıyor…' : 'Doğrula ve yeniden say' }}
      </button>
    </details>

    <ErrorBox v-if="store.segmentError" :message="store.segmentError.message" :details="store.segmentError.details" />

    <div class="actions">
      <button type="button" @click="store.step = 'brief'">Geri</button>
      <button class="primary" type="button" :disabled="!reachable || store.segmentLoading" @click="store.generateContent()">
        İçerik üret
      </button>
    </div>
    <p v-if="!reachable" class="hint">Bu kitlede ulaşılabilen kimse yok.</p>
  </section>
</template>
