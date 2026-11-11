<script setup lang="ts">
import { computed } from 'vue'
import { EXAMPLE_BRIEF, useCampaignStore } from '../stores/campaign'
import { countChars } from '../lib/template'
import ErrorBox from './ErrorBox.vue'

const MAX_BRIEF = 2000
const store = useCampaignStore()
const length = computed(() => countChars(store.brief.trim()))
const canSubmit = computed(() => length.value > 0 && length.value <= MAX_BRIEF && !store.segmentLoading)
</script>

<template>
  <section class="card">
    <h2>Kampanyayı anlat</h2>
    <p class="hint">Kime, ne göndermek istediğini kendi cümlelerinle yaz. Kitleyi ve metinleri bundan çıkaracağız; hiçbir şey onayın olmadan gönderilmez.</p>

    <form @submit.prevent="store.generateSegment()">
      <label for="brief">Brief</label>
      <textarea
        id="brief"
        v-model="store.brief"
        rows="5"
        placeholder="Örn. Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder."
      />
      <div class="row between">
        <button class="ghost" type="button" @click="store.brief = EXAMPLE_BRIEF">Örnek brief'i kullan</button>
        <span class="counter" :class="{ over: length > MAX_BRIEF }">{{ length }} / {{ MAX_BRIEF }}</span>
      </div>

      <ErrorBox v-if="store.segmentError" :message="store.segmentError.message" :details="store.segmentError.details" />

      <div class="actions">
        <button class="primary" type="submit" :disabled="!canSubmit">
          {{ store.segmentLoading ? 'Kitle çıkarılıyor…' : 'Kitleyi bul' }}
        </button>
      </div>
    </form>
  </section>
</template>
