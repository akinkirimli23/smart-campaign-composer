<script setup lang="ts">
import { computed } from 'vue'
import { EXAMPLE_BRIEFS, useCampaignStore } from '../stores/campaign'
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
      <div class="row between">
        <label for="brief">Brief</label>
        <span class="counter" :class="{ over: length > MAX_BRIEF }">{{ length }} / {{ MAX_BRIEF }}</span>
      </div>
      <textarea
        id="brief"
        v-model="store.brief"
        rows="5"
        placeholder="Örn. Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder."
      />

      <p class="hint tight">Bir örnekle başla:</p>
      <div class="examples">
        <button v-for="e in EXAMPLE_BRIEFS" :key="e.label" class="chip" type="button" @click="store.brief = e.text">
          {{ e.label }}
        </button>
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
