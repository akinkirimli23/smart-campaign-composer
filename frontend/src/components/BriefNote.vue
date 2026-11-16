<script setup lang="ts">
// The brief the marketer wrote, kept in sight on the steps that follow it.
import { useCampaignStore } from '../stores/campaign'

const store = useCampaignStore()
</script>

<template>
  <aside v-if="store.brief.trim()" class="brief-note" aria-label="Brief">
    <span class="brief-note-label">Brief</span>
    <p class="brief-note-text">{{ store.brief }}</p>
    <button class="ghost" type="button" @click="store.step = 'brief'">Düzenle</button>
  </aside>
</template>
