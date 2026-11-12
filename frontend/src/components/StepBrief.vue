<script setup lang="ts">
import { computed } from 'vue'
import { EXAMPLE_BRIEFS, useCampaignStore } from '../stores/campaign'
import { countChars } from '../lib/template'
import AppIcon from './AppIcon.vue'
import ErrorBox from './ErrorBox.vue'

const MAX_BRIEF = 2000
const store = useCampaignStore()
const length = computed(() => countChars(store.brief.trim()))
const canSubmit = computed(() => length.value > 0 && length.value <= MAX_BRIEF && !store.segmentLoading)

const how = [
  { title: 'Kitleyi buluruz', text: 'Brief’ten kimlere gideceğini çıkarır, kaç kişi olduğunu gösteririz.' },
  { title: 'Mesajları yazarız', text: 'SMS ve email metinleri ekrana akar; istediğin gibi düzenlersin.' },
  { title: 'Sen onaylarsın', text: 'Hemen gönder ya da ileri bir saate zamanla.' },
  { title: 'Canlı izlersin', text: 'Kaç mesajın teslim edildiğini anlık görürsün.' },
]
</script>

<template>
  <div class="page-head">
    <div>
      <h1>Kampanyanı anlat</h1>
      <p class="lead">Kime, ne göndermek istediğini kendi cümlelerinle yaz. Gerisini adım adım birlikte hazırlayacağız.</p>
    </div>
  </div>

  <div class="layout">
    <section class="card">
      <form @submit.prevent="store.generateSegment()">
        <div class="label-row">
          <label for="brief">Brief</label>
          <span class="counter" :class="{ over: length > MAX_BRIEF }">{{ length }} / {{ MAX_BRIEF }}</span>
        </div>
        <textarea
          id="brief"
          v-model="store.brief"
          rows="7"
          placeholder="Örn. Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder."
        />

        <p class="section-label">Ya da bir örnekle başla</p>
        <div class="examples">
          <button v-for="e in EXAMPLE_BRIEFS" :key="e.label" class="example" type="button" @click="store.brief = e.text">
            <strong>{{ e.label }}</strong>
            <span>{{ e.text }}</span>
          </button>
        </div>

        <ErrorBox v-if="store.segmentError" style="margin-top: 16px" :message="store.segmentError.message" :details="store.segmentError.details" />

        <div class="actions">
          <button class="primary" type="submit" :disabled="!canSubmit">
            <span v-if="store.segmentLoading" class="spinner" aria-hidden="true" />
            {{ store.segmentLoading ? 'Kitle çıkarılıyor…' : 'Kitleyi bul' }}
            <AppIcon v-if="!store.segmentLoading" name="arrow-right" :size="18" />
          </button>
        </div>
      </form>
    </section>

    <aside class="card">
      <h2>Nasıl çalışır?</h2>
      <ol class="how-list">
        <li v-for="(h, i) in how" :key="h.title">
          <span class="num">{{ i + 1 }}</span>
          <div>
            <strong>{{ h.title }}</strong>
            <span>{{ h.text }}</span>
          </div>
        </li>
      </ol>
      <p class="note">Onayın olmadan hiçbir şey gönderilmez.</p>
    </aside>
  </div>
</template>
