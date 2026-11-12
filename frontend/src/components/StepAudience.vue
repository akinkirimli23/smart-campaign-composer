<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useCampaignStore } from '../stores/campaign'
import type { Rule } from '../api/types'
import { formatDateTime, fromLocalInput } from '../lib/time'
import AppIcon from './AppIcon.vue'
import ErrorBox from './ErrorBox.vue'
import RuleEditor from './RuleEditor.vue'

const store = useCampaignStore()
const segment = computed(() => store.segment)
const audience = computed(() => segment.value?.audience)

// Assumptions belong to the condition they explain.
function assumptionsFor(index: number) {
  return (segment.value?.assumptions ?? []).filter((a) => a.condition_index === index)
}

const editing = ref(false)
onMounted(() => store.loadRuleOptions())

function startEditing() {
  store.segmentError = null
  editing.value = true
}

async function apply(rule: Rule) {
  // Stay in the editor on refusal, with the backend's errors next to the rows.
  if (await store.applyRule(rule)) editing.value = false
}

function cancel() {
  store.segmentError = null
  editing.value = false
}

// Developer view: the rule as the backend sees it. Open the app with ?debug.
const debug = new URLSearchParams(window.location.search).has('debug')
const ruleDraft = ref('')
watch(
  () => segment.value?.rule,
  (rule) => {
    if (rule) ruleDraft.value = JSON.stringify(rule, null, 2)
  },
  { immediate: true },
)

const suggestedAt = computed(() => {
  const at = store.scheduleSuggestion ? fromLocalInput(store.scheduleSuggestion.value) : null
  return at ? formatDateTime(at) : ''
})

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

  <section v-else-if="segment && segment.rule && audience" class="card">
    <h2>Kitle</h2>

    <template v-if="editing && store.ruleOptions">
      <p class="hint">Koşulları değiştir, ekle ya da kaldır. Uyguladığında kural yeniden doğrulanır ve kişi sayıları güncellenir.</p>
      <RuleEditor
        :rule="segment.rule"
        :options="store.ruleOptions"
        :busy="store.segmentLoading"
        :errors="store.segmentError?.details ?? []"
        @apply="apply"
        @cancel="cancel"
      />
      <p v-if="store.segmentError && !store.segmentError.details.length" class="alert error" role="alert">{{ store.segmentError.message }}</p>
    </template>

    <template v-else>
      <p class="hint">Bu açıklama kuraldan türetilir; gönderimde tam olarak bu kitle kullanılır.</p>

      <div class="row between">
        <p class="summary">{{ segment.description?.summary }}</p>
        <button class="ghost" type="button" :disabled="!store.ruleOptions" @click="startEditing">Koşulları düzenle</button>
      </div>
      <ul class="conditions">
        <li v-for="(line, i) in segment.description?.conditions ?? []" :key="i">
          {{ line }}
          <p v-for="a in assumptionsFor(i)" :key="a.phrase" class="assumption">
            <span class="tag">Yorum</span> “{{ a.phrase }}” → {{ a.interpretation }}
          </p>
        </li>
      </ul>

      <dl class="stats">
        <div class="stat">
          <span class="icon"><AppIcon name="users" /></span>
          <div>
            <dt>Kurala uyan</dt>
            <dd>{{ audience.total }}</dd>
          </div>
        </div>
        <div class="stat sms">
          <span class="icon"><AppIcon name="sms" /></span>
          <div>
            <dt>SMS gidecek</dt>
            <dd>{{ audience.sms }}</dd>
          </div>
        </div>
        <div class="stat email">
          <span class="icon"><AppIcon name="email" /></span>
          <div>
            <dt>Email gidecek</dt>
            <dd>{{ audience.email }}</dd>
          </div>
        </div>
        <div class="stat off">
          <span class="icon"><AppIcon name="user-off" /></span>
          <div>
            <dt>Ulaşılamayan</dt>
            <dd>{{ audience.unreachable }}</dd>
          </div>
        </div>
      </dl>
      <p class="hint">Bir kanaldan gönderim için hem izin hem adres gerekir. Ulaşılamayanlar iki kanalda da bunlardan birine sahip değil.</p>
      <p v-if="segment.languages && segment.languages.length > 1" class="hint">
        Ulaşılabilenlerin dili:
        <template v-for="(l, i) in segment.languages" :key="l.language">{{ i ? ' · ' : ' ' }}{{ l.label }} {{ l.reachable }} kişi</template>.
        İçerik önce {{ segment.languages[0]?.label }} yazılır; diğer diller içerik adımında tek seferde eklenir ve herkes kendi dilindeki metni alır.
      </p>

      <details v-if="debug" class="advanced">
        <summary>Geliştirici görünümü: kural (JSON)</summary>
        <textarea v-model="ruleDraft" rows="12" spellcheck="false" class="mono" aria-label="Kural JSON" />
        <button type="button" :disabled="store.segmentLoading" @click="store.recountSegment(ruleDraft)">
          {{ store.segmentLoading ? 'Doğrulanıyor…' : 'Doğrula ve yeniden say' }}
        </button>
      </details>

      <p v-if="store.scheduleSuggestion" class="alert info">
        <strong>Gönderim zamanı önerisi:</strong> {{ suggestedAt }}
        (brief'teki “{{ store.scheduleSuggestion.phrase }}”). Onay adımında görür ve değiştirebilirsin.
      </p>

      <ErrorBox v-if="store.segmentError" :message="store.segmentError.message" :details="store.segmentError.details" />

      <div class="actions">
        <button type="button" @click="store.step = 'brief'">Geri</button>
        <button class="primary" type="button" :disabled="!reachable || store.segmentLoading" @click="store.generateContent()">
          İçerik üret
        </button>
      </div>
      <p v-if="!reachable" class="hint">Bu kitlede ulaşılabilen kimse yok.</p>
    </template>
  </section>
</template>
