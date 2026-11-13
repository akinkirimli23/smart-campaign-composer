<script setup lang="ts">
// The home page: a pinned scene that the visitor plays by scrolling, then a
// few quiet editorial sections.
//
// One number drives everything in the scene: the scroll progress through the
// pinned section (lib/story.ts). Every frame the drawing and the typography
// are both derived from it, so they stay in step, and scrolling back rewinds
// the story. There is no video file: the "film" is a field of dots, one per
// person, drawn on a canvas.
import { onMounted, onUnmounted, ref, type Directive } from 'vue'
import { useCampaignStore } from '../stores/campaign'
import { activeChapter, chapterPhase, CHAPTERS, lerp, sceneAt, scrollProgress, smoothstep, typedCount } from '../lib/story'
import AppIcon from './AppIcon.vue'

const store = useCampaignStore()

const BRIEF = 'Son 30 günde sepetine ürün ekleyip almayan İstanbul’daki kullanıcılara %30 indirim hatırlat.'
// The example brief on the seed data: the numbers the product really shows.
const AUDIENCE = 381
const MESSAGES = 476

const hero = ref<HTMLElement | null>(null)
const canvas = ref<HTMLCanvasElement | null>(null)
const chapterEls = ref<HTMLElement[]>([])
const typed = ref('')
const audienceCount = ref(0)
const delivered = ref(0)
const current = ref(0)
// With "reduce motion" the scene is not pinned and nothing animates.
const still = ref(false)

// --- the field of dots ------------------------------------------------------------
interface Field {
  count: number
  // Unit coordinates inside the scene's region.
  baseX: Float32Array
  baseY: Float32Array
  clusterX: Float32Array
  clusterY: Float32Array
  cardX: Float32Array
  cardY: Float32Array
  phase: Float32Array
  delay: Float32Array
  selected: Uint8Array
}

/** A small seeded generator: the same field on every visit. */
function random(seed: number) {
  let s = seed
  return () => {
    s = (s * 1664525 + 1013904223) % 4294967296
    return s / 4294967296
  }
}

function buildField(count: number): Field {
  const rand = random(42)
  const f: Field = {
    count,
    baseX: new Float32Array(count),
    baseY: new Float32Array(count),
    clusterX: new Float32Array(count),
    clusterY: new Float32Array(count),
    cardX: new Float32Array(count),
    cardY: new Float32Array(count),
    phase: new Float32Array(count),
    delay: new Float32Array(count),
    selected: new Uint8Array(count),
  }
  const chosen: number[] = []
  for (let i = 0; i < count; i++) {
    f.baseX[i] = rand()
    f.baseY[i] = rand()
    f.phase[i] = rand() * Math.PI * 2
    if (rand() < 0.3) {
      f.selected[i] = 1
      chosen.push(i)
    }
  }
  // The audience gathers into a disc (a sunflower packing reads as "a crowd")
  // and then into the lines of a message: a few rows of uneven length.
  const rows = [1, 0.92, 0.97, 0.6, 0, 0.88, 0.95, 0.45]
  const perRow = rows.map((w) => Math.round(w * 100))
  const cells = perRow.reduce((sum, n) => sum + n, 0)
  chosen.forEach((index, k) => {
    const r = Math.sqrt((k + 0.5) / chosen.length) * 0.5
    const angle = k * 2.39996323
    f.clusterX[index] = 0.5 + r * Math.cos(angle) * 0.62
    f.clusterY[index] = 0.5 + r * Math.sin(angle)

    let cell = Math.floor((k / chosen.length) * cells)
    let row = 0
    while (row < rows.length - 1 && cell >= (perRow[row] ?? 0)) {
      cell -= perRow[row] ?? 0
      row++
    }
    const width = rows[row] ?? 1
    const along = (cell + rand()) / Math.max(1, perRow[row] ?? 1)
    f.cardX[index] = 0.2 + along * width * 0.6
    f.cardY[index] = 0.28 + (row / (rows.length - 1)) * 0.44 + (rand() - 0.5) * 0.012
    // Messages leave from left to right, with a little disorder.
    f.delay[index] = Math.min(1, (f.cardX[index] ?? 0) * 0.8 + rand() * 0.2)
  })
  return f
}

let field: Field | null = null
let width = 0
let height = 0
// The scene's region in pixels: the right part on wide screens, the top part on narrow ones.
let region = { x: 0, y: 0, w: 0, h: 0 }
let narrow = false

function resize() {
  const el = canvas.value
  if (!el) return
  const box = el.getBoundingClientRect()
  width = box.width
  height = box.height
  const ratio = Math.min(window.devicePixelRatio || 1, 2)
  el.width = Math.round(width * ratio)
  el.height = Math.round(height * ratio)
  el.getContext('2d')?.setTransform(ratio, 0, 0, ratio, 0, 0)

  narrow = width < 820
  region = narrow
    ? { x: width * 0.06, y: height * 0.1, w: width * 0.88, h: height * 0.4 }
    : { x: width * 0.44, y: height * 0.16, w: width * 0.5, h: height * 0.68 }
  // Fewer dots on small screens: the story is the same, the cost is not.
  const count = narrow ? 420 : 1100
  if (!field || field.count !== count) field = buildField(count)
}

const INK = '20, 19, 15'
const ACCENT = '217, 65, 28'

function draw(p: number, time: number) {
  const ctx = canvas.value?.getContext('2d')
  if (!ctx || !field) return
  const scene = sceneAt(p)
  const f = field
  const size = narrow ? 2 : 2.4
  ctx.clearRect(0, 0, width, height)

  // The people who do not match: present, then quietly out of focus.
  ctx.fillStyle = `rgba(${INK}, ${lerp(0.42, 0.14, scene.select) * (1 - scene.send * 0.6)})`
  for (let i = 0; i < f.count; i++) {
    if (f.selected[i]) continue
    const x = region.x + (f.baseX[i] ?? 0) * region.w + Math.sin(time * 0.4 + (f.phase[i] ?? 0)) * 3
    const y = region.y + (f.baseY[i] ?? 0) * region.h + Math.cos(time * 0.33 + (f.phase[i] ?? 0)) * 3
    ctx.fillRect(x, y, size, size)
  }

  // The outline of the message the audience turns into.
  const card = scene.card * (1 - smoothstep(0, 0.5, scene.send))
  if (card > 0.01) {
    ctx.strokeStyle = `rgba(${INK}, ${card * 0.3})`
    ctx.lineWidth = 1
    ctx.beginPath()
    const box: [number, number, number, number] = [region.x + region.w * 0.12, region.y + region.h * 0.16, region.w * 0.76, region.h * 0.68]
    if (typeof ctx.roundRect === 'function') ctx.roundRect(...box, 14)
    else ctx.rect(...box)
    ctx.stroke()
  }

  // The audience. Its colour is the story's one accent.
  const drift = 1 - scene.gather * 0.85
  const travel = region.w * 0.9 + (narrow ? width * 0.3 : width * 0.2)
  for (let i = 0; i < f.count; i++) {
    if (!f.selected[i]) continue
    const phase = f.phase[i] ?? 0
    let x = (f.baseX[i] ?? 0) + (Math.sin(time * 0.4 + phase) * 3 * drift) / region.w
    let y = (f.baseY[i] ?? 0) + (Math.cos(time * 0.33 + phase) * 3 * drift) / region.h
    x = lerp(x, f.clusterX[i] ?? 0, scene.gather)
    y = lerp(y, f.clusterY[i] ?? 0, scene.gather)
    x = lerp(x, f.cardX[i] ?? 0, scene.card)
    y = lerp(y, f.cardY[i] ?? 0, scene.card)

    // Each message leaves in its own time; the whole audience is gone at the end.
    const gone = smoothstep(0, 1, scene.send * 1.7 - (f.delay[i] ?? 0) * 0.7)
    const px = region.x + x * region.w + gone * gone * travel
    const py = region.y + y * region.h + Math.sin(gone * Math.PI) * -region.h * 0.06
    const alpha = lerp(0.42, 1, scene.select) * (1 - smoothstep(0.75, 1, gone))
    if (alpha <= 0.01) continue

    ctx.fillStyle = scene.select < 1 ? `rgba(${mix(scene.select)}, ${alpha})` : `rgba(${ACCENT}, ${alpha})`
    if (gone > 0.02 && gone < 0.98) {
      // A short trail while in flight.
      ctx.fillRect(px - 14 * gone, py + size / 2 - 0.5, 14 * gone, 1)
    }
    ctx.fillRect(px, py, size, size)
  }
}

/** The dot colour on its way from "everyone" to "the audience". */
function mix(t: number): string {
  const from = [20, 19, 15]
  const to = [217, 65, 28]
  return from.map((c, i) => Math.round(lerp(c, to[i] ?? c, t))).join(', ')
}

// --- typography in step with the scene ------------------------------------------
function typeset(p: number) {
  CHAPTERS.forEach((chapter, i) => {
    const el = chapterEls.value[i]
    if (!el) return
    const { enter, exit } = chapterPhase(p, chapter)
    el.style.setProperty('--enter', enter.toFixed(4))
    el.style.setProperty('--exit', exit.toFixed(4))
    el.toggleAttribute('data-off', enter <= 0 || exit >= 1)
  })
  const scene = sceneAt(p)
  const text = BRIEF.slice(0, typedCount(scene.type, BRIEF.length))
  if (text !== typed.value) typed.value = text
  const people = Math.round(AUDIENCE * scene.gather)
  if (people !== audienceCount.value) audienceCount.value = people
  const sent = Math.round(MESSAGES * scene.send)
  if (sent !== delivered.value) delivered.value = sent
  const chapter = activeChapter(p)
  if (chapter !== current.value) current.value = chapter
  hero.value?.style.setProperty('--progress', p.toFixed(4))
}

// --- the loop ----------------------------------------------------------------------
let frame = 0
let visible = false
let progress = 0
let observer: IntersectionObserver | null = null

function target(): number {
  const el = hero.value
  if (!el) return 0
  const box = el.getBoundingClientRect()
  return scrollProgress(box.top, box.height, window.innerHeight)
}

function tick(now: number) {
  // The scene follows the scroll position with a short ease, so a notchy
  // mouse wheel still reads as one continuous movement.
  const goal = target()
  progress = Math.abs(goal - progress) < 0.0004 ? goal : lerp(progress, goal, 0.14)
  typeset(progress)
  draw(progress, now / 1000)
  if (visible) frame = requestAnimationFrame(tick)
}

function start() {
  if (visible) return
  visible = true
  progress = target()
  frame = requestAnimationFrame(tick)
}

function stop() {
  visible = false
  cancelAnimationFrame(frame)
}

function onResize() {
  resize()
  if (!visible) draw(progress, 0)
}

onMounted(() => {
  still.value = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  if (still.value) {
    // Everything on the page at once, nothing moving.
    typed.value = BRIEF
    audienceCount.value = AUDIENCE
    delivered.value = MESSAGES
    return
  }
  resize()
  typeset(target())
  window.addEventListener('resize', onResize)
  // The loop only runs while the scene is on screen.
  observer = new IntersectionObserver((entries) => (entries[0]?.isIntersecting ? start() : stop()))
  if (hero.value) observer.observe(hero.value)
})

onUnmounted(() => {
  stop()
  observer?.disconnect()
  window.removeEventListener('resize', onResize)
})

function begin() {
  // A fresh brief, unless a campaign is already being composed.
  if (store.step === 'dashboard') store.reset()
  else store.navigate({ name: 'compose' })
  window.scrollTo(0, 0)
}

const principles = [
  { title: 'Yapay zekâ önerir.', text: 'Claude, yazdığın cümleden kitle kuralını ve mesaj metinlerini çıkarır. Öneridir; karar değildir.' },
  {
    title: 'Kurallar doğrular.',
    text: 'Her çıktı şemaya, kapalı değer kümelerine ve karakter sınırlarına karşı kontrol edilir. Uymuyorsa nedeniyle birlikte geri gönderilir.',
  },
  { title: 'İnsan onaylar.', text: 'Hiçbir mesaj sen okumadan, düzeltmeden ve onaylamadan kuyruğa girmez.' },
]

const figures = [
  { value: '160', label: 'SMS karakteri. Alıcının adı yerine konduktan sonra ölçülür.' },
  { value: '80', label: 'Email konusu karakteri. Aynı ölçüyle.' },
  { value: '100', unit: '/sn', label: 'Kanal başına gönderim hızı. Üstüne çıkılmaz.' },
  { value: '1', unit: ' kez', label: 'Aynı kişiye aynı mesaj. Yeniden denemede bile.' },
]

const questions = [
  {
    q: 'Yapay zekâ veritabanına dokunuyor mu?',
    a: 'Hayır. Model yalnızca yapılandırılmış bir kural önerir. Sorguyu, doğrulanmış kuraldan sistem kendisi kurar; modelin yazdığı hiçbir metin sorguya girmez.',
  },
  {
    q: 'İzni olmayan birine mesaj gider mi?',
    a: 'Gitmez. Bir kanaldan gönderim için hem izin hem adres gerekir ve bu, gönderim anında yeniden kontrol edilir.',
  },
  { q: 'Müşterim başka bir dil okuyorsa?', a: 'Metin, kitledeki her dil için yazılır. Herkes kendi dilindeki mesajı alır.' },
  { q: 'Yanlışlıkla onaylarsam?', a: 'Zamanlanmış ya da süren bir kampanya durdurulabilir. Bekleyen mesajlar gönderilmez.' },
  { q: 'Gerçek kişilere mesaj gidiyor mu?', a: 'Bu kurulumda hayır. Alıcılar örnek veridir, gönderim bir test sağlayıcısına yapılır.' },
]

const chapterLabels = ['Giriş', 'Brief', 'Kitle', 'Mesaj', 'Gönderim']

// v-reveal: an element rises into place the first time it scrolls into view.
const vReveal: Directive<HTMLElement> = {
  mounted(el) {
    if (typeof IntersectionObserver === 'undefined' || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    el.classList.add('reveal')
    const seen = new IntersectionObserver(
      (entries) => {
        if (!entries[0]?.isIntersecting) return
        el.classList.add('in')
        seen.disconnect()
      },
      { rootMargin: '0px 0px -12% 0px' },
    )
    seen.observe(el)
  },
}
</script>

<template>
  <div class="home">
    <section ref="hero" class="story" :class="{ still }" aria-label="Nasıl çalışır">
      <div class="stage">
        <canvas v-if="!still" ref="canvas" class="film" aria-hidden="true" />

        <div class="chapters">
          <article :ref="(el) => (chapterEls[0] = el as HTMLElement)" class="chapter">
            <p class="eyebrow"><span>Smart Campaign Composer</span></p>
            <h1 class="display">
              <span class="line"><span>Bir cümle yaz.</span></span>
              <span class="line"><span>Kampanya <em>olsun.</em></span></span>
            </h1>
            <p class="deck"><span>Brief’ten kitleye, mesaja ve gönderime.</span></p>
            <p class="cta">
              <button class="pill signal" type="button" @click="begin">Kampanya oluştur <AppIcon name="arrow-right" :size="18" /></button>
              <small>ya da nasıl çalıştığını görmek için kaydır ↓</small>
            </p>
          </article>

          <article :ref="(el) => (chapterEls[1] = el as HTMLElement)" class="chapter">
            <p class="eyebrow"><span>Böl. 1 — Brief</span></p>
            <h2 class="display">
              <span class="line"><span>Ne istediğini</span></span>
              <span class="line"><span><em>kendi cümlenle</em> anlat.</span></span>
            </h2>
            <p class="deck"><span>Form yok, filtre yok. Kime ne göndereceğini yazman yeter.</span></p>
            <p class="typed" aria-hidden="true">{{ typed }}<span class="caret" /></p>
          </article>

          <article :ref="(el) => (chapterEls[2] = el as HTMLElement)" class="chapter">
            <p class="eyebrow"><span>Böl. 2 — Kitle</span></p>
            <h2 class="display">
              <span class="line"><span>Kitle,</span></span>
              <span class="line"><span><em>kendini</em> gösterir.</span></span>
            </h2>
            <p class="deck"><span>Cümlen doğrulanmış bir kurala döner. Kaç kişiye, hangi kanaldan gideceğini önceden görürsün.</span></p>
            <p class="figure"><strong>{{ audienceCount }}</strong> kişi · 222 SMS · 254 email</p>
          </article>

          <article :ref="(el) => (chapterEls[3] = el as HTMLElement)" class="chapter">
            <p class="eyebrow"><span>Böl. 3 — Mesaj</span></p>
            <h2 class="display">
              <span class="line"><span>Metin yazılır.</span></span>
              <span class="line"><span>Son söz <em>sende.</em></span></span>
            </h2>
            <p class="deck"><span>SMS ve email ekrana akar. Okur, düzeltir, öyle onaylarsın.</span></p>
          </article>

          <article :ref="(el) => (chapterEls[4] = el as HTMLElement)" class="chapter">
            <p class="eyebrow"><span>Böl. 4 — Gönderim</span></p>
            <h2 class="display">
              <span class="line"><span>Onayla.</span></span>
              <span class="line"><span>Akışı <em>izle.</em></span></span>
            </h2>
            <p class="deck"><span>Saniyede en fazla yüz mesaj; kimseye iki kez değil. Her teslimat canlı sayılır.</span></p>
            <p class="figure"><strong>{{ delivered }}</strong> / {{ MESSAGES }} teslim edildi</p>
            <p class="cta"><button class="pill light" type="button" @click="begin">Kampanya oluştur <AppIcon name="arrow-right" :size="18" /></button></p>
          </article>
        </div>

        <ol v-if="!still" class="rail" aria-hidden="true">
          <li v-for="(label, i) in chapterLabels" :key="label" :class="{ on: i === current }">
            <span>{{ String(i).padStart(2, '0') }}</span> {{ label }}
          </li>
        </ol>
        <div v-if="!still" class="progress" aria-hidden="true"><span /></div>
      </div>
    </section>

    <section class="band">
      <div class="container">
        <p class="eyebrow dark">Model</p>
        <div class="principles">
          <article v-for="p in principles" :key="p.title" v-reveal>
            <h3>{{ p.title }}</h3>
            <p>{{ p.text }}</p>
          </article>
        </div>
      </div>
    </section>

    <section class="band">
      <div class="container">
        <p class="eyebrow dark">Sınırlar</p>
        <h2 v-reveal class="statement">Hız değil, <em>güven</em> için tasarlandı.</h2>
        <dl class="figures">
          <div v-for="f in figures" :key="f.label" v-reveal>
            <dt>{{ f.value }}<small v-if="f.unit">{{ f.unit }}</small></dt>
            <dd>{{ f.label }}</dd>
          </div>
        </dl>
      </div>
    </section>

    <section class="band">
      <div class="container split">
        <div>
          <p class="eyebrow dark">Sorular</p>
          <h2 v-reveal class="statement small">Daha önce <em>soruldu.</em></h2>
        </div>
        <div class="faq">
          <details v-for="item in questions" :key="item.q">
            <summary>{{ item.q }}</summary>
            <p>{{ item.a }}</p>
          </details>
        </div>
      </div>
    </section>

    <section class="closing">
      <div class="container">
        <h2 v-reveal class="display">Bir brief <em>yaz.</em></h2>
        <p class="deck">Gerisi dört adım.</p>
        <button class="pill" type="button" @click="begin">Kampanya oluştur <AppIcon name="arrow-right" :size="18" /></button>
      </div>
    </section>
  </div>
</template>
