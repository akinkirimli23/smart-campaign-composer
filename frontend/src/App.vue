<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useCampaignStore, type Step } from './stores/campaign'
import { formatHash, parseHash, sameRoute } from './lib/route'
import { chooseTheme, otherTheme, storedTheme } from './lib/theme'
import AppIcon from './components/AppIcon.vue'
import CampaignList from './components/CampaignList.vue'
import HomePage from './components/HomePage.vue'
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

// Which section of the site is on screen.
const home = computed(() => store.view === 'home')
const composing = computed(() => store.view === 'compose' && !store.standalone)
const listing = computed(() => store.view === 'campaigns' || (store.view === 'compose' && store.standalone))

function showComposer() {
  // From the list: back to whatever was being composed. From a campaign
  // opened from the list: a fresh start.
  if (store.standalone) store.reset()
  else store.navigate({ name: 'compose' })
}

// Cream unless the visitor chose the dark theme.
const theme = ref(storedTheme() ?? 'light')
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

// A new screen starts at its top, not where the previous one was scrolled to.
watch(
  () => [store.view, store.step],
  () => window.scrollTo(0, 0),
)

onMounted(() => {
  followHash()
  window.addEventListener('hashchange', followHash)
})
onUnmounted(() => window.removeEventListener('hashchange', followHash))
</script>

<template>
  <header class="site-header" :class="{ 'on-film': home }">
    <div class="container">
      <button class="brand" type="button" aria-label="Ana sayfa" @click="store.showHome()">Composer<i>.</i></button>

      <nav class="site-nav" aria-label="Gezinme">
        <button class="nav-link" :class="{ active: listing }" type="button" :aria-current="listing ? 'page' : undefined" @click="store.showCampaigns()">
          Kampanyalar
        </button>
      </nav>

      <div class="header-actions">
        <button v-if="!composing" class="primary small header-cta" type="button" @click="showComposer">
          Kampanya oluştur <AppIcon name="arrow-right" :size="16" />
        </button>
        <button
          v-if="composing && store.step !== 'brief'"
          class="small with-icon"
          type="button"
          title="Bu kampanyayı bırak ve boş bir brief ile başla"
          @click="store.reset()"
        >
          <AppIcon name="plus" :size="16" /> <span class="label">Yeni kampanya</span>
        </button>
        <button
          v-if="!home"
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

  <HomePage v-if="home" />

  <div v-else class="container page">
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
            <span class="dot">0{{ i + 1 }}</span>
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
