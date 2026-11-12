<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useCampaignStore, type Step } from './stores/campaign'
import { formatHash, parseHash, sameRoute } from './lib/route'
import { chooseTheme, otherTheme, storedTheme, systemTheme } from './lib/theme'
import AppIcon from './components/AppIcon.vue'
import CampaignList from './components/CampaignList.vue'
import StepBrief from './components/StepBrief.vue'
import StepAudience from './components/StepAudience.vue'
import StepContent from './components/StepContent.vue'
import StepDashboard from './components/StepDashboard.vue'

const store = useCampaignStore()

const steps: { id: Step; label: string; sub: string }[] = [
  { id: 'brief', label: 'Brief', sub: 'Kampanyayı anlat' },
  { id: 'audience', label: 'Kitle', sub: 'Kime gidecek' },
  { id: 'content', label: 'Mesajlar', sub: 'Ne gidecek, ne zaman' },
  { id: 'dashboard', label: 'Gönderim', sub: 'Canlı takip' },
]

function position(id: Step): number {
  return steps.findIndex((s) => s.id === id)
}

// A finished step can be revisited from the stepper, until the campaign is
// approved: after that there is nothing left to change.
const busy = computed(() => store.segmentLoading || store.streaming || store.localizing || store.approving)
function canRevisit(index: number): boolean {
  return index < position(store.step) && store.step !== 'dashboard' && !busy.value
}

// Which of the two sections of the site is on screen.
const composing = computed(() => store.view === 'compose' && !store.standalone)

function showComposer() {
  // From the list: back to whatever was being composed. From a campaign
  // opened from the list: a fresh start.
  if (store.standalone) store.reset()
  else store.navigate({ name: 'compose' })
}

const theme = ref(storedTheme() ?? systemTheme())
function toggleTheme() {
  theme.value = otherTheme(theme.value)
  chooseTheme(theme.value)
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
  <header class="site-header">
    <div class="container">
      <button class="brand" type="button" aria-label="Ana sayfa" @click="showComposer">
        <span class="brand-mark"><AppIcon name="megaphone" :size="18" /></span>
        <span class="brand-name">Campaign Composer</span>
      </button>

      <nav class="site-nav" aria-label="Gezinme">
        <button class="nav-link" :class="{ active: composing }" type="button" :aria-current="composing ? 'page' : undefined" @click="showComposer">
          <AppIcon name="sparkle" :size="18" /> Kampanya oluştur
        </button>
        <button
          class="nav-link"
          :class="{ active: !composing }"
          type="button"
          :aria-current="!composing ? 'page' : undefined"
          @click="store.showCampaigns()"
        >
          <AppIcon name="list" :size="18" /> Kampanyalar
        </button>
      </nav>

      <div class="header-actions">
        <button
          v-if="composing && store.step !== 'brief'"
          class="small with-icon"
          type="button"
          title="Bu kampanyayı bırak ve boş bir brief ile başla"
          @click="store.reset()"
        >
          <AppIcon name="plus" :size="16" /> Yeni kampanya
        </button>
        <button
          class="icon-button"
          type="button"
          :aria-label="theme === 'dark' ? 'Açık temaya geç' : 'Koyu temaya geç'"
          :title="theme === 'dark' ? 'Açık tema' : 'Koyu tema'"
          @click="toggleTheme"
        >
          <AppIcon :name="theme === 'dark' ? 'sun' : 'moon'" :size="18" />
        </button>
      </div>
    </div>
  </header>

  <div class="container page">
    <main v-if="store.view === 'campaigns'">
      <CampaignList />
    </main>

    <template v-else>
      <!-- A campaign opened from the list did not come through the steps. -->
      <ol v-if="!store.standalone" class="stepper" aria-label="Adımlar">
        <li
          v-for="(s, i) in steps"
          :key="s.id"
          :class="{ current: s.id === store.step, done: i < position(store.step), upcoming: i > position(store.step) }"
          :aria-current="s.id === store.step ? 'step' : undefined"
        >
          <button class="step" type="button" :disabled="!canRevisit(i)" @click="store.step = s.id">
            <span class="dot">
              <AppIcon v-if="i < position(store.step)" name="check" :size="16" />
              <template v-else>{{ i + 1 }}</template>
            </span>
            <span class="step-text">
              <span class="step-title">{{ s.label }}</span>
              <span class="step-sub">{{ s.sub }}</span>
            </span>
          </button>
        </li>
      </ol>

      <main>
        <StepBrief v-if="store.step === 'brief'" />
        <StepAudience v-else-if="store.step === 'audience'" />
        <StepContent v-else-if="store.step === 'content'" />
        <StepDashboard v-else />
      </main>
    </template>
  </div>

  <footer class="site-footer">Gönderimler test sağlayıcısına yapılır; gerçek kimseye mesaj gitmez.</footer>
</template>
