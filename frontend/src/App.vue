<script setup lang="ts">
import { useCampaignStore, type Step } from './stores/campaign'
import AppIcon from './components/AppIcon.vue'
import StepBrief from './components/StepBrief.vue'
import StepAudience from './components/StepAudience.vue'
import StepContent from './components/StepContent.vue'
import StepDashboard from './components/StepDashboard.vue'

const store = useCampaignStore()

const steps: { id: Step; label: string }[] = [
  { id: 'brief', label: 'Brief' },
  { id: 'audience', label: 'Kitle' },
  { id: 'content', label: 'İçerik ve onay' },
  { id: 'dashboard', label: 'Gönderim' },
]

function position(id: Step): number {
  return steps.findIndex((s) => s.id === id)
}
</script>

<template>
  <header class="topbar">
    <div class="brand">
      <span class="brand-mark"><AppIcon name="megaphone" /></span>
      <div>
        <h1>Smart Campaign Composer</h1>
        <p>Brief'ten kitleye, mesaja ve gönderime</p>
      </div>
    </div>
    <button v-if="store.step !== 'brief'" class="ghost" type="button" @click="store.reset()">Yeni kampanya</button>
  </header>

  <ol class="stepper" aria-label="Adımlar">
    <li
      v-for="(s, i) in steps"
      :key="s.id"
      :class="{ current: s.id === store.step, done: i < position(store.step) }"
      :aria-current="s.id === store.step ? 'step' : undefined"
    >
      <span class="dot">
        <AppIcon v-if="i < position(store.step)" name="check" />
        <template v-else>{{ i + 1 }}</template>
      </span>
      {{ s.label }}
    </li>
  </ol>

  <main>
    <StepBrief v-if="store.step === 'brief'" />
    <StepAudience v-else-if="store.step === 'audience'" />
    <StepContent v-else-if="store.step === 'content'" />
    <StepDashboard v-else />
  </main>
</template>
