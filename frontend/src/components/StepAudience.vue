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

// Share of each language among the reachable audience, for the bars.
const languageShares = computed(() => {
  const languages = segment.value?.languages ?? []
  const total = languages.reduce((sum, l) => sum + l.reachable, 0)
  return languages.map((l) => ({ ...l, percent: total ? Math.round((l.reachable / total) * 100) : 0 }))
})
</script>

<template>
  <template v-if="segment?.status === 'unsupported'">
    <div class="page-head">
      <div>
        <p class="eyebrow">Böl. 2 — Kitle</p>
        <h1>Kitle <em>çıkarılamadı.</em></h1>
        <p class="lead">Brief'teki bir istek, kitle kurallarıyla ifade edilemiyor.</p>
      </div>
    </div>
    <section class="card">
      <div class="alert warn" role="alert">
        <strong>{{ segment.unsupported?.reason }}</strong>
        <ul v-if="segment.unsupported?.phrases.length">
          <li v-for="p in segment.unsupported.phrases" :key="p">“{{ p }}”</li>
        </ul>
      </div>
      <p class="hint tight">Eksik koşullu bir kural daha geniş bir kitleye gönderim demek olurdu, bu yüzden kısmi bir kural önermiyoruz. Brief'i sadeleştirip yeniden deneyebilirsin.</p>
      <div class="actions">
        <button class="primary" type="button" @click="store.step = 'brief'"><AppIcon name="arrow-left" :size="18" /> Brief'i düzenle</button>
      </div>
    </section>
  </template>

  <template v-else-if="segment && segment.rule && audience">
    <div class="page-head">
      <div>
        <p class="eyebrow">Böl. 2 — Kitle</p>
        <h1>Hedef <em>kitle.</em></h1>
        <p class="lead">Brief'inden çıkardığımız kitle bu. Uygun değilse koşulları düzenleyebilirsin; gönderimde tam olarak bu kitle kullanılır.</p>
      </div>
    </div>

    <div class="layout">
      <section class="card">
        <template v-if="editing && store.ruleOptions">
          <div class="card-head">
            <h2 class="card-title"><span class="icon"><AppIcon name="edit" /></span> Koşulları düzenle</h2>
          </div>
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
          <div class="card-head">
            <h2 class="card-title"><span class="icon"><AppIcon name="users" /></span> Kimlere gidecek?</h2>
            <button class="small with-icon" type="button" :disabled="!store.ruleOptions" @click="startEditing">
              <AppIcon name="edit" :size="16" /> Koşulları düzenle
            </button>
          </div>

          <p class="summary">{{ segment.description?.summary }}</p>
          <ul class="conditions">
            <li v-for="(line, i) in segment.description?.conditions ?? []" :key="i">
              <span class="check"><AppIcon name="check" :size="14" /></span>
              <div>
                {{ line }}
                <p v-for="a in assumptionsFor(i)" :key="a.phrase" class="assumption">
                  <span class="tag">Yorum</span> “{{ a.phrase }}” → {{ a.interpretation }}
                </p>
              </div>
            </li>
          </ul>

          <details v-if="debug" class="advanced">
            <summary>Geliştirici görünümü: kural (JSON)</summary>
            <textarea v-model="ruleDraft" rows="12" spellcheck="false" class="mono" aria-label="Kural JSON" />
            <button type="button" :disabled="store.segmentLoading" @click="store.recountSegment(ruleDraft)">
              {{ store.segmentLoading ? 'Doğrulanıyor…' : 'Doğrula ve yeniden say' }}
            </button>
          </details>

          <ErrorBox v-if="store.segmentError" style="margin-top: 16px" :message="store.segmentError.message" :details="store.segmentError.details" />
        </template>
      </section>

      <aside class="stack">
        <section class="card">
          <h2>Kitle büyüklüğü</h2>
          <p class="big-number" style="margin-top: 10px">{{ audience.total }}</p>
          <p class="muted">kişi kurala uyuyor</p>
          <ul class="reach">
            <li class="sms">
              <span class="icon"><AppIcon name="sms" /></span> SMS gidecek
              <span class="value">{{ audience.sms }}</span>
            </li>
            <li class="email">
              <span class="icon"><AppIcon name="email" /></span> Email gidecek
              <span class="value">{{ audience.email }}</span>
            </li>
            <li class="off">
              <span class="icon"><AppIcon name="user-off" /></span> Ulaşılamayan
              <span class="value">{{ audience.unreachable }}</span>
            </li>
          </ul>
          <p class="hint tight">Bir kanaldan gönderim için hem izin hem adres gerekir.</p>
        </section>

        <section v-if="languageShares.length > 1" class="card">
          <h2 class="card-title"><span class="icon"><AppIcon name="globe" /></span> Diller</h2>
          <div class="language-bars">
            <div v-for="l in languageShares" :key="l.language" class="language-bar">
              <div class="row">
                <span>{{ l.label }}</span>
                <span class="muted">{{ l.reachable }} kişi · %{{ l.percent }}</span>
              </div>
              <div class="meter"><span :style="{ width: l.percent + '%' }" /></div>
            </div>
          </div>
          <p class="hint tight" style="margin-top: 12px">Herkes kendi dilindeki mesajı alır. Metin önce {{ languageShares[0]?.label }} yazılır, diğer diller sonraki adımda eklenir.</p>
        </section>

        <section v-if="store.scheduleSuggestion" class="card">
          <h2 class="card-title"><span class="icon"><AppIcon name="clock" /></span> Gönderim zamanı önerisi</h2>
          <p style="margin-top: 6px"><strong>{{ suggestedAt }}</strong></p>
          <p class="hint tight">Brief'teki “{{ store.scheduleSuggestion.phrase }}” ifadesinden. Sonraki adımda değiştirebilirsin.</p>
        </section>
      </aside>
    </div>

    <div v-if="!editing" class="actionbar">
      <p class="actionbar-info">
        <template v-if="reachable"><strong>{{ audience.sms }} SMS</strong> ve <strong>{{ audience.email }} email</strong> gönderilecek.</template>
        <template v-else>Bu kitlede ulaşılabilen kimse yok.</template>
      </p>
      <div class="actionbar-buttons">
        <button type="button" @click="store.step = 'brief'">Geri</button>
        <button class="primary" type="button" :disabled="!reachable || store.segmentLoading" @click="store.generateContent()">
          Mesajları yaz <AppIcon name="arrow-right" :size="18" />
        </button>
      </div>
    </div>
  </template>
</template>
