<script setup lang="ts">
import { computed } from 'vue'
import { useCampaignStore } from '../stores/campaign'
import ErrorBox from './ErrorBox.vue'

const store = useCampaignStore()

const variables = computed(() => Object.entries(store.contentMeta?.variables ?? {}))
const editable = computed(() => !store.streaming && store.contentMeta !== null)
const audience = computed(() => store.segment?.audience)

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

    <template v-if="!store.contentError">
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
          rows="3"
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
          rows="8"
          :readonly="!editable"
          :aria-invalid="editable && store.measured.body.problems.length > 0"
        />
        <p v-for="p in editable ? store.measured.body.problems : []" :key="p" class="field-error">{{ p }}</p>
        <p v-for="p in approveErrorsFor('email.body')" :key="p" class="field-error">{{ p }}</p>
      </div>

      <p v-if="variables.length" class="hint">
        Kullanılabilir değişkenler:
        <template v-for="([name, length], i) in variables" :key="name">
          <code v-text="'{{' + name + '}}'" /> ({{ length }} karakter sayılır){{ i < variables.length - 1 ? ', ' : '' }}
        </template>
      </p>
    </template>

    <ErrorBox
      v-if="store.approveError"
      :message="store.approveError.message"
      :details="store.approveError.details.filter((d) => !['sms', 'email.subject', 'email.body'].includes(d.path))"
    />

    <div class="actions">
      <button type="button" :disabled="store.streaming || store.approving" @click="store.step = 'audience'">Geri</button>
      <button type="button" :disabled="store.streaming || store.approving" @click="store.generateContent()">Yeniden üret</button>
      <button class="primary" type="button" :disabled="!store.contentValid || store.streaming || store.approving" @click="store.approve()">
        {{ store.approving ? 'Kuyruğa alınıyor…' : 'Onayla ve gönder' }}
      </button>
    </div>
    <p v-if="audience && store.contentMeta" class="hint">
      Onaylarsan {{ audience.sms }} SMS ve {{ audience.email }} email kuyruğa alınır. Gönderim test sağlayıcısına yapılır; gerçek kimseye mesaj gitmez.
    </p>
  </section>
</template>
