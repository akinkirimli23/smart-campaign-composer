<script setup lang="ts">
import { computed } from 'vue'
import { useCampaignStore } from '../stores/campaign'
import { renderSample, SAMPLE_RECIPIENT } from '../lib/template'
import ErrorBox from './ErrorBox.vue'

const store = useCampaignStore()

const variables = computed(() => Object.entries(store.contentMeta?.variables ?? {}))
const editable = computed(() => !store.streaming && store.contentMeta !== null)
const audience = computed(() => store.segment?.audience)

// What one recipient would receive, with a made-up person filled in.
const preview = computed(() => ({
  sms: renderSample(store.content.sms),
  subject: renderSample(store.content.email.subject),
  body: renderSample(store.content.email.body),
}))
const sampleName = `${SAMPLE_RECIPIENT.first_name} ${SAMPLE_RECIPIENT.last_name}, ${SAMPLE_RECIPIENT.city}`

// Earliest selectable start, in the form datetime-local expects (local time,
// minutes). Only a hint for the picker; the store and the backend check too.
const earliest = (() => {
  const now = new Date(Date.now() + 60_000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}T${pad(now.getHours())}:${pad(now.getMinutes())}`
})()

/** Server-side errors for one field, from a refused approval. */
function approveErrorsFor(path: string): string[] {
  return (store.approveError?.details ?? []).filter((d) => d.path === path).map((d) => d.message)
}
</script>

<template>
  <section class="card">
    <h2>İçerik</h2>

    <p v-if="store.streaming" class="status" role="status">
      <span class="spinner" aria-hidden="true" />
      {{ store.attempt > 1 ? `${store.attempt}. deneme yazılıyor…` : 'Metinler yazılıyor…' }}
    </p>
    <p v-else-if="store.contentMeta" class="hint">
      Metinleri düzenleyebilirsin. Sayaçlar, değişkenler en uzun değerleriyle doldurulmuş halini ölçer; onayda sunucu aynı kontrolü yeniden yapar.
    </p>

    <!-- A rejected attempt is shown, not hidden: it explains the wait. -->
    <div v-for="r in store.rejections" :key="r.attempt" class="alert warn">
      <strong>{{ r.attempt }}. deneme doğrulamadan geçemedi{{ store.streaming || store.contentMeta ? ', yeniden yazdırıldı' : '' }}</strong>
      <ul>
        <li v-for="(e, i) in r.errors" :key="i"><code>{{ e.path }}</code> {{ e.message }}</li>
      </ul>
    </div>

    <ErrorBox v-if="store.contentError" :message="store.contentError.message" :details="store.contentError.details" />

    <div v-if="!store.contentError" class="compose">
      <div>
        <div class="field">
          <div class="row between">
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
            v-model="store.content.sms"
            rows="4"
            :readonly="!editable"
            :aria-invalid="editable && (store.measured.sms.problems.length > 0 || store.measured.sms.length > store.limits.sms)"
          />
          <p v-for="p in editable ? store.measured.sms.problems : []" :key="p" class="field-error">{{ p }}</p>
          <p v-for="p in approveErrorsFor('sms')" :key="p" class="field-error">{{ p }}</p>
        </div>

        <div class="field">
          <div class="row between">
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
            v-model="store.content.email.subject"
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
            v-model="store.content.email.body"
            rows="9"
            :readonly="!editable"
            :aria-invalid="editable && store.measured.body.problems.length > 0"
          />
          <p v-for="p in editable ? store.measured.body.problems : []" :key="p" class="field-error">{{ p }}</p>
          <p v-for="p in approveErrorsFor('email.body')" :key="p" class="field-error">{{ p }}</p>
        </div>

        <template v-if="variables.length">
          <p class="hint tight">Kullanılabilir değişkenler (en uzun değerleriyle sayılır):</p>
          <div class="variables">
            <span v-for="[name, length] in variables" :key="name">
              <code v-text="'{{' + name + '}}'" /> · {{ length }} karakter
            </span>
          </div>
        </template>
      </div>

      <aside class="preview" aria-label="Önizleme">
        <p class="who">Önizleme · örnek alıcı: {{ sampleName }}</p>

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

    <ErrorBox
      v-if="store.approveError"
      :message="store.approveError.message"
      :details="store.approveError.details.filter((d) => !['sms', 'email.subject', 'email.body'].includes(d.path))"
    />

    <fieldset v-if="store.contentMeta" class="schedule">
      <legend>Ne zaman gönderilsin?</legend>
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
        <p class="hint tight">
          Alıcılar şimdi belirlenir; o saate kadar izni kalkan olursa atlanır. Gönderim başlayana kadar iptal edebilirsin.
        </p>
      </template>
    </fieldset>

    <div class="actions">
      <button type="button" :disabled="store.streaming || store.approving" @click="store.step = 'audience'">Geri</button>
      <button type="button" :disabled="store.streaming || store.approving" @click="store.generateContent()">Yeniden üret</button>
      <button
        class="primary"
        type="button"
        :disabled="!store.contentValid || store.streaming || store.approving || store.scheduleProblem !== null"
        @click="store.approve()"
      >
        {{ store.approving ? 'Kuyruğa alınıyor…' : store.sendMode === 'later' ? 'Onayla ve zamanla' : 'Onayla ve gönder' }}
      </button>
    </div>
    <p v-if="audience && store.contentMeta" class="hint">
      Onaylarsan {{ audience.sms }} SMS ve {{ audience.email }} email kuyruğa alınır. Gönderim test sağlayıcısına yapılır; gerçek kimseye mesaj gitmez.
    </p>
  </section>
</template>
