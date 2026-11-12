<script setup lang="ts">
import { onMounted, onUnmounted, watch } from 'vue'
import { useCampaignStore, type Step } from './stores/campaign'
import { formatHash, parseHash, sameRoute } from './lib/route'
import AppIcon from './components/AppIcon.vue'
import CampaignList from './components/CampaignList.vue'
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

// The URL hash and the store are kept in step in both directions: a reload or
// the back button moves the app, and moving in the app updates the address.
function followHash() {
  const route = parseHash(window.location.hash)
  if (!sameRoute(route, store.route)) store.navigate(route)
}

watch(
  () => store.route,
  (route) => {
    const hash = formatHash(route)
    if (window.location.hash === hash) return
    if (hash) window.location.hash = hash
    // Leaving for the composer: drop the hash without adding a history entry.
    else history.replaceState(null, '', window.location.pathname + window.location.search)
  },
)

onMounted(() => {
  followHash()
  window.addEventListener('hashchange', followHash)
})
onUnmounted(() => window.removeEventListener('hashchange', followHash))
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
    <nav class="row" aria-label="Gezinme">
      <button v-if="store.view !== 'campaigns'" class="ghost" type="button" @click="store.showCampaigns()">Kampanyalar</button>
      <button v-if="store.view === 'compose' && store.step !== 'brief'" class="ghost" type="button" @click="store.reset()">
        Yeni kampanya
      </button>
    </nav>
  </header>

  <main v-if="store.view === 'campaigns'" class="standalone">
    <CampaignList />
  </main>

  <template v-else>
    <!-- A campaign opened from the list did not come through the steps. -->
    <ol v-if="!store.standalone" class="stepper" aria-label="Adımlar">
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

    <main :class="{ standalone: store.standalone }">
      <StepBrief v-if="store.step === 'brief'" />
      <StepAudience v-else-if="store.step === 'audience'" />
      <StepContent v-else-if="store.step === 'content'" />
      <StepDashboard v-else />
    </main>
  </template>
</template>
