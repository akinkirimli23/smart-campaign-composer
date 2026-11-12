<script setup lang="ts">
import { computed } from 'vue'
import { useCampaignStore } from '../stores/campaign'
import { renderSample, SAMPLE_RECIPIENT } from '../lib/template'
import type { FieldError } from '../api/types'
import AppIcon from './AppIcon.vue'
import ErrorBox from './ErrorBox.vue'

const store = useCampaignStore()

const variables = computed(() => Object.entries(store.contentMeta?.variables ?? {}))
const busy = computed(() => store.streaming || store.localizing)
const editable = computed(() => !busy.value && store.contentMeta !== null)
const audience = computed(() => store.segment?.audience)

// The texts in the editor: the main language's, or another language's.
const text = computed(() => store.activeContent)

// What one recipient would receive, with a made-up person filled in.
const preview = computed(() => ({
  sms: renderSample(text.value.sms),
  subject: renderSample(text.value.email.subject),
  body: renderSample(text.value.email.body),
}))
const sampleName = `${SAMPLE_RECIPIENT.first_name} ${SAMPLE_RECIPIENT.last_name}, ${SAMPLE_RECIPIENT.city}`

// --- languages ------------------------------------------------------------------
// Tabs appear only when the audience reads more than one language.
const multilingual = computed(() => store.languages.length > 1)
const mainLabel = computed(() => store.languageLabel(store.contentLanguage))

function labels(codes: string[]): string {
  return codes.map((code) => store.languageLabel(code)).join(', ')
}

const missing = computed(() => labels(store.missingLanguages))
const written = computed(() => labels(Object.keys(store.variants)))

/** Reachable people who read a language that has no text of its own. */
const missingReaders = computed(() =>
  store.languages.filter((l) => store.missingLanguages.includes(l.language)).reduce((sum, l) => sum + l.reachable, 0),
)

// Earliest selectable start, in the form datetime-local expects (local time,
// minutes). Only a hint for the picker; the store and the backend check too.
const earliest = (() => {
  const now = new Date(Date.now() + 60_000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}T${pad(now.getHours())}:${pad(now.getMinutes())}`
})()

// --- errors of a refused approval ---------------------------------------------
// The backend names the language in the path: "variants.en.sms".
const FIELDS = ['sms', 'email.subject', 'email.body']

function splitPath(path: string): { language: string; field: string } | null {
  const match = /^variants\.([^.]+)\.(.+)$/.exec(path)
  return match ? { language: match[1] ?? '', field: match[2] ?? '' } : null
}

/** Server-side errors for one field of the language being edited. */
function approveErrorsFor(field: string): string[] {
  return (store.approveError?.details ?? [])
    .filter((d) => {
      const at = splitPath(d.path)
      return at ? at.language === store.activeLanguage && at.field === field : d.path === field
    })
    .map((d) => d.message)
}

/** Everything else: errors of the other languages (named), and non-field errors. */
const otherApproveErrors = computed<FieldError[]>(() =>
  (store.approveError?.details ?? []).flatMap((d) => {
    const at = splitPath(d.path)
    if (!at) return FIELDS.includes(d.path) ? [] : [d]
    if (at.language === store.activeLanguage && FIELDS.includes(at.field)) return []
    return [{ path: `${store.languageLabel(at.language)} · ${at.field}`, message: d.message }]
  }),
)
</script>

<template>
  <div class="page-head">
    <div>
      <h1>Mesajlar</h1>
      <p class="lead">
        Metinleri oku, istediğin gibi düzenle ve ne zaman gideceğini seç. Sayaçlar, değişkenler en uzun değerleriyle doldurulmuş halini ölçer.
      </p>
    </div>
  </div>

  <!-- A rejected attempt is shown, not hidden: it explains the wait. -->
  <div v-for="r in store.rejections" :key="r.attempt" class="alert warn">
    <strong>{{ r.attempt }}. deneme doğrulamadan geçemedi{{ store.streaming || store.contentMeta ? ', yeniden yazdırıldı' : '' }}</strong>
    <ul>
      <li v-for="(e, i) in r.errors" :key="i"><code>{{ e.path }}</code> {{ e.message }}</li>
    </ul>
  </div>

  <ErrorBox v-if="store.contentError" :message="store.contentError.message" :details="store.contentError.details" />

  <section v-if="!store.contentError" class="card">
    <div class="card-head">
      <!-- One tab per language of the audience. Each recipient gets their own. -->
      <div v-if="multilingual && store.contentMeta" class="tabs" role="tablist" aria-label="Metnin dili">
        <button
          v-for="l in store.languages"
          :key="l.language"
          type="button"
          role="tab"
          class="tab"
          :class="{ active: store.activeLanguage === l.language }"
          :aria-selected="store.activeLanguage === l.language"
          :disabled="!store.hasText(l.language)"
          @click="store.activeLanguage = l.language"
        >
          {{ l.label }}
          <span class="count">{{ l.reachable }} kişi</span>
          <span v-if="!store.hasText(l.language)" class="tag">metin yok</span>
          <span v-else-if="!busy && store.invalidLanguages.includes(l.language)" class="tag bad">düzeltilmeli</span>
        </button>
      </div>
      <h2 v-else class="card-title"><span class="icon"><AppIcon name="edit" /></span> Metinler</h2>

      <p v-if="store.streaming" class="status" style="margin: 0" role="status">
        <span class="spinner" aria-hidden="true" />
        {{ store.attempt > 1 ? `${store.attempt}. deneme yazılıyor…` : multilingual ? `${mainLabel} metinler yazılıyor…` : 'Metinler yazılıyor…' }}
      </p>
      <p v-else-if="store.localizing" class="status" style="margin: 0" role="status">
        <span class="spinner" aria-hidden="true" />
        {{ store.localizeAttempt > 1 ? `${store.localizeAttempt}. deneme: ` : '' }}{{ mainLabel }} metinden diğer diller yazılıyor…
      </p>
      <button
        v-else-if="multilingual && store.contentMeta && store.missingLanguages.length"
        class="primary small"
        type="button"
        @click="store.localize()"
      >
        <AppIcon name="globe" :size="16" /> {{ missing }} metnini yaz
      </button>
      <button v-else-if="multilingual && store.contentMeta && store.translationsStale" class="small with-icon" type="button" @click="store.localize()">
        <AppIcon name="globe" :size="16" /> {{ written }} metnini yeniden yaz
      </button>
    </div>

    <template v-if="multilingual && store.contentMeta && !busy">
      <p v-if="store.missingLanguages.length" class="alert info">
        Kitlede <strong>{{ missing }}</strong> okuyan <strong>{{ missingReaders }} kişi</strong> var. Önce {{ mainLabel }} metni son haline getir, sonra
        “{{ missing }} metnini yaz” ile onlara kendi dillerinde metin yazdır. Kampanya yalnızca {{ mainLabel }} okuyanlara gidecekse kitle adımına dön ve
        “Dil: {{ mainLabel }}” koşulunu ekle.
      </p>
      <p v-else-if="store.translationsStale" class="alert warn">
        {{ mainLabel }} metin değişti; {{ written }} metin önceki halinden yazılmıştı. Yeniden yazdırabilir ya da elle güncelleyebilirsin.
      </p>
    </template>

    <div v-for="r in store.localizeRejections" :key="'l' + r.attempt" class="alert warn">
      <strong>Diğer diller: {{ r.attempt }}. deneme doğrulamadan geçemedi{{ store.localizing || !store.localizeError ? ', yeniden yazdırıldı' : '' }}</strong>
      <ul>
        <li v-for="(e, i) in r.errors" :key="i"><code>{{ e.path }}</code> {{ e.message }}</li>
      </ul>
    </div>
    <ErrorBox v-if="store.localizeError" :message="store.localizeError.message" :details="store.localizeError.details" />

    <div class="compose">
      <div>
        <div class="field">
          <div class="label-row">
            <label for="sms">SMS</label>
            <span
              v-if="store.contentMeta"
              class="counter"
              :class="{ over: store.measured.sms.length > store.limits.sms }"
              aria-live="polite"
            >{{ store.measured.sms.length }} / {{ store.limits.sms }}</span>
          </div>
          <textarea
            id="sms"
            v-model="text.sms"
            rows="4"
            :readonly="!editable"
            :aria-invalid="editable && (store.measured.sms.problems.length > 0 || store.measured.sms.length > store.limits.sms)"
          />
          <p v-for="p in editable ? store.measured.sms.problems : []" :key="p" class="field-error">{{ p }}</p>
          <p v-for="p in approveErrorsFor('sms')" :key="p" class="field-error">{{ p }}</p>
        </div>

        <div class="field">
          <div class="label-row">
            <label for="subject">Email konusu</label>
            <span
              v-if="store.contentMeta"
              class="counter"
              :class="{ over: store.measured.subject.length > store.limits.email_subject }"
              aria-live="polite"
            >{{ store.measured.subject.length }} / {{ store.limits.email_subject }}</span>
          </div>
          <input
            id="subject"
            v-model="text.email.subject"
            type="text"
            :readonly="!editable"
            :aria-invalid="editable && (store.measured.subject.problems.length > 0 || store.measured.subject.length > store.limits.email_subject)"
          />
          <p v-for="p in editable ? store.measured.subject.problems : []" :key="p" class="field-error">{{ p }}</p>
          <p v-for="p in approveErrorsFor('email.subject')" :key="p" class="field-error">{{ p }}</p>
        </div>

        <div class="field">
          <label for="body">Email metni</label>
          <textarea
            id="body"
            v-model="text.email.body"
            rows="9"
            :readonly="!editable"
            :aria-invalid="editable && store.measured.body.problems.length > 0"
          />
          <p v-for="p in editable ? store.measured.body.problems : []" :key="p" class="field-error">{{ p }}</p>
          <p v-for="p in approveErrorsFor('email.body')" :key="p" class="field-error">{{ p }}</p>
        </div>

        <template v-if="variables.length">
          <p class="hint tight">Kişiye özel alanlar (en uzun değerleriyle sayılır):</p>
          <div class="variables">
            <span v-for="[name, length] in variables" :key="name">
              <code v-text="'{{' + name + '}}'" /> · {{ length }} karakter
            </span>
          </div>
        </template>
      </div>

      <aside class="preview" aria-label="Önizleme">
        <p class="who">
          <strong>Önizleme<template v-if="multilingual"> · {{ store.languageLabel(store.activeLanguage) }}</template></strong>
          Örnek alıcı: {{ sampleName }}
        </p>

        <h3>SMS</h3>
        <div class="phone">
          <p class="sender">Mağaza</p>
          <p class="bubble">
            <template v-if="preview.sms">{{ preview.sms }}</template>
            <span v-else class="placeholder">Metin burada görünecek</span>
          </p>
        </div>

        <h3>Email</h3>
        <div class="mail">
          <p class="subject">
            <template v-if="preview.subject">{{ preview.subject }}</template>
            <span v-else class="placeholder">Konu</span>
          </p>
          <p class="body">
            <template v-if="preview.body">{{ preview.body }}</template>
            <span v-else class="placeholder">Metin burada görünecek</span>
          </p>
        </div>
      </aside>
    </div>
  </section>

  <section v-if="store.contentMeta" class="card" style="margin-top: 20px">
    <div class="card-head">
      <h2 class="card-title"><span class="icon"><AppIcon name="clock" /></span> Ne zaman gönderilsin?</h2>
    </div>
    <fieldset class="schedule">
      <label class="choice"><input v-model="store.sendMode" type="radio" value="now" /> Hemen</label>
      <label class="choice"><input v-model="store.sendMode" type="radio" value="later" /> Zamanla</label>
      <template v-if="store.sendMode === 'later'">
        <input
          v-model="store.scheduledFor"
          type="datetime-local"
          :min="earliest"
          aria-label="Gönderim tarihi ve saati"
          :aria-invalid="store.scheduleProblem !== null && store.scheduledFor !== ''"
        />
        <p v-if="store.scheduleProblem && store.scheduledFor !== ''" class="field-error">{{ store.scheduleProblem }}</p>
        <!-- Shown while the field still holds what was read from the brief. -->
        <p v-if="store.scheduleSuggestion && store.scheduledFor === store.scheduleSuggestion.value" class="assumption suggested">
          <span class="tag">Brief'ten</span> “{{ store.scheduleSuggestion.phrase }}” → {{ store.scheduleSuggestion.interpretation }}
          Değiştirebilirsin.
        </p>
        <p class="hint tight">
          Alıcılar şimdi belirlenir; o saate kadar izni kalkan olursa atlanır. Gönderim başlayana kadar iptal edebilirsin.
        </p>
      </template>
    </fieldset>
  </section>

  <ErrorBox v-if="store.approveError" style="margin-top: 20px" :message="store.approveError.message" :details="otherApproveErrors" />

  <div class="actionbar">
    <p class="actionbar-info">
      <template v-if="audience && store.contentMeta">
        <strong>{{ audience.sms }} SMS</strong> ve <strong>{{ audience.email }} email</strong> kuyruğa alınacak.
        <template v-if="multilingual && !store.missingLanguages.length">Her alıcı kendi dilindeki metni alır.</template>
        <template v-else-if="multilingual">Onaylamak için önce {{ missing }} metnini yazdır.</template>
      </template>
      <template v-else-if="busy">Metinler hazırlanıyor…</template>
    </p>
    <div class="actionbar-buttons">
      <button type="button" :disabled="busy || store.approving" @click="store.step = 'audience'">Geri</button>
      <button type="button" :disabled="busy || store.approving" @click="store.generateContent()">Yeniden üret</button>
      <button
        class="primary"
        type="button"
        :disabled="!store.contentValid || !store.languagesResolved || busy || store.approving || store.scheduleProblem !== null"
        @click="store.approve()"
      >
        <span v-if="store.approving" class="spinner" aria-hidden="true" />
        <AppIcon v-else name="send" :size="18" />
        {{ store.approving ? 'Kuyruğa alınıyor…' : store.sendMode === 'later' ? 'Onayla ve zamanla' : 'Onayla ve gönder' }}
      </button>
    </div>
  </div>
</template>
