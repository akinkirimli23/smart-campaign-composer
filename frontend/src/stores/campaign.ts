import { computed, reactive, ref } from 'vue'
import { defineStore } from 'pinia'
import { api, ApiError } from '../api/client'
import { measure } from '../lib/template'
import { fromLocalInput, toLocalInput } from '../lib/time'
import type { Route } from '../lib/route'
import type {
  CampaignCreated,
  CampaignStatus,
  CampaignSummary,
  Content,
  ContentEvent,
  ContentField,
  ContentResult,
  FieldError,
  Lengths,
  LocalizedContent,
  LocalizeEvent,
  MessageStatus,
  Recipient,
  RuleOptions,
  SegmentResponse,
} from '../api/types'

export type Step = 'brief' | 'audience' | 'content' | 'dashboard'

export const EXAMPLE_BRIEF =
  'Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul bölgesindeki kullanıcılara %30 indirim hatırlatması gönder.'

/** Starting points offered on the first screen. */
export const EXAMPLE_BRIEFS: { label: string; text: string }[] = [
  { label: 'Sepeti terk edenler', text: EXAMPLE_BRIEF },
  {
    label: 'Sadık müşteriler',
    text: "İstanbul ve Ankara'da 1000 TL üzeri harcayan kullanıcıları yeni sezon ön satışına davet et. Özel hissettirsin.",
  },
  { label: 'Yeni üyeler', text: 'Son 7 günde kayıt olanlara hoş geldin mesajı ve ilk alışverişe %10 indirim gönder.' },
]

/** One rejected generation attempt, shown so the retry is not a mystery. */
export interface Rejection {
  attempt: number
  errors: FieldError[]
}

/** The language of a campaign whose audience names none. */
const DEFAULT_LANGUAGE = 'tr'

function emptyContent(): Content {
  return { sms: '', email: { subject: '', body: '' } }
}

/** A plain copy: what is sent must not be the object the form edits. */
function copyContent(c: Content): Content {
  return { sms: c.sms, email: { subject: c.email.subject, body: c.email.body } }
}

function setField(c: Content, field: ContentField, value: string) {
  if (field === 'sms') c.sms = value
  else if (field === 'email.subject') c.email.subject = value
  else c.email.body = value
}

function getField(c: Content, field: ContentField): string {
  if (field === 'sms') return c.sms
  return field === 'email.subject' ? c.email.subject : c.email.body
}

function describe(err: unknown): { message: string; details: FieldError[] } {
  if (err instanceof ApiError) return { message: err.message, details: err.details }
  return { message: 'Beklenmeyen bir hata oluştu', details: [] }
}

/**
 * The whole campaign flow: brief -> audience -> content -> approval -> live
 * dashboard. Components only render this state and call these actions.
 */
export const useCampaignStore = defineStore('campaign', () => {
  const step = ref<Step>('brief')
  const brief = ref('')

  // "home" is the landing page, "compose" the four-step flow, "campaigns"
  // the list of campaigns.
  const view = ref<'home' | 'compose' | 'campaigns'>('compose')
  // The dashboard shows a campaign opened from the list or a link, not one
  // that just came through the steps.
  const standalone = ref(false)

  // --- audience ---------------------------------------------------------------
  const segment = ref<SegmentResponse | null>(null)
  const segmentLoading = ref(false)
  const segmentError = ref<{ message: string; details: FieldError[] } | null>(null)

  async function generateSegment() {
    segmentLoading.value = true
    segmentError.value = null
    try {
      segment.value = await api.generateSegment(brief.value)
      applyScheduleSuggestion(segment.value.schedule)
      step.value = 'audience'
    } catch (err) {
      segmentError.value = describe(err)
    } finally {
      segmentLoading.value = false
    }
  }

  // Allowed values and bounds for the rule editor; loaded once.
  const ruleOptions = ref<RuleOptions | null>(null)

  async function loadRuleOptions() {
    if (ruleOptions.value) return
    try {
      ruleOptions.value = await api.segmentOptions()
    } catch (err) {
      segmentError.value = describe(err)
    }
  }

  /**
   * Validates and counts a rule the marketer edited. Returns whether the
   * backend accepted it; on refusal the previous rule and counts stay.
   */
  async function applyRule(rule: unknown): Promise<boolean> {
    segmentLoading.value = true
    segmentError.value = null
    try {
      // The edited rule has no model assumptions any more; the backend's
      // response carries only what it derived from the rule itself.
      segment.value = await api.countSegment(rule)
      return true
    } catch (err) {
      segmentError.value = describe(err)
      return false
    } finally {
      segmentLoading.value = false
    }
  }

  /** The same, from raw JSON (the developer view, ?debug). */
  async function recountSegment(ruleJSON: string) {
    let rule: unknown
    try {
      rule = JSON.parse(ruleJSON)
    } catch {
      segmentError.value = { message: 'Kural geçerli JSON değil', details: [] }
      return
    }
    await applyRule(rule)
  }

  // --- content ----------------------------------------------------------------
  // The audience's languages, largest first (DECISIONS 30). The texts are
  // written once in the first one ("content" below), then adapted to the
  // others in a single further request ("variants").
  const languages = computed(() => segment.value?.languages ?? [])

  function languageLabel(code: string): string {
    return languages.value.find((l) => l.language === code)?.label ?? code.toUpperCase()
  }

  const content = reactive<Content>(emptyContent())
  // The language "content" is written in: the audience's largest when it
  // was generated.
  const contentLanguage = ref(DEFAULT_LANGUAGE)
  const streaming = ref(false)
  const attempt = ref(0)
  const rejections = ref<Rejection[]>([])
  const contentMeta = ref<Omit<ContentResult, 'content'> | null>(null)
  const contentError = ref<{ message: string; details: FieldError[] } | null>(null)
  let abortContent: AbortController | null = null

  function clearContent() {
    Object.assign(content, emptyContent())
  }

  function onContentEvent(e: ContentEvent) {
    switch (e.event) {
      case 'attempt':
        // A new attempt replaces the previous one's text entirely.
        attempt.value = e.data.attempt
        clearContent()
        break
      case 'delta':
        setField(content, e.data.field, getField(content, e.data.field) + e.data.text)
        break
      case 'rejected':
        rejections.value.push({ attempt: e.data.attempt, errors: e.data.errors })
        break
      case 'result': {
        // Only this event carries validated content; it replaces the preview.
        const { content: validated, ...meta } = e.data
        Object.assign(content, copyContent(validated))
        contentMeta.value = meta
        break
      }
      case 'error':
        clearContent()
        contentError.value = { message: e.data.message, details: e.data.details ?? [] }
        break
    }
  }

  async function generateContent() {
    abortContent?.abort()
    const controller = new AbortController()
    abortContent = controller
    step.value = 'content'
    streaming.value = true
    attempt.value = 0
    rejections.value = []
    contentMeta.value = null
    contentError.value = null
    approveError.value = null
    clearContent()
    // The other languages were written from the text that is being replaced.
    clearLocalization()
    contentLanguage.value = languages.value[0]?.language ?? DEFAULT_LANGUAGE
    activeLanguage.value = contentLanguage.value
    try {
      await api.streamContent(brief.value, contentLanguage.value, onContentEvent, controller.signal)
      if (!contentMeta.value && !contentError.value) {
        contentError.value = { message: 'Akış sonuç gelmeden kesildi', details: [] }
      }
    } catch (err) {
      if (controller.signal.aborted) return // superseded by a newer request
      clearContent()
      contentError.value = describe(err)
    } finally {
      if (abortContent === controller) streaming.value = false
    }
  }

  // --- other languages ----------------------------------------------------------
  // Texts for the audience's other languages, by language code. Empty until
  // the marketer asks for them; a recipient whose language is missing here
  // receives the main text.
  const variants = reactive<Record<string, Content>>({})
  const localizing = ref(false)
  const localizeAttempt = ref(0)
  const localizeRejections = ref<Rejection[]>([])
  const localizeError = ref<{ message: string; details: FieldError[] } | null>(null)
  // The main text the variants were written from, to notice when it changes.
  const localizedFrom = ref<string | null>(null)
  let abortLocalize: AbortController | null = null

  // The language shown in the editor.
  const activeLanguage = ref(DEFAULT_LANGUAGE)

  function clearVariants() {
    for (const code of Object.keys(variants)) delete variants[code]
  }

  function clearLocalization() {
    abortLocalize?.abort()
    abortLocalize = null
    clearVariants()
    localizing.value = false
    localizeAttempt.value = 0
    localizeRejections.value = []
    localizeError.value = null
    localizedFrom.value = null
  }

  function hasText(language: string): boolean {
    return language === contentLanguage.value || language in variants
  }

  /** The texts being edited: the main ones, or one of the other languages. */
  const activeContent = computed<Content>(() => variants[activeLanguage.value] ?? content)

  /** Audience languages that have no text of their own yet. */
  const missingLanguages = computed(() => languages.value.map((l) => l.language).filter((code) => !hasText(code)))

  // Approval stays closed until every language of the audience has a text
  // (DECISIONS 30). There is no "send the main language to everyone" switch:
  // a campaign meant for one language narrows its audience by language.
  const languagesResolved = computed(() => missingLanguages.value.length === 0)

  /** The main text was edited after the other languages were written from it. */
  const translationsStale = computed(
    () => localizedFrom.value !== null && Object.keys(variants).length > 0 && localizedFrom.value !== JSON.stringify(copyContent(content)),
  )

  function onLocalizeEvent(e: LocalizeEvent, source: Content) {
    switch (e.event) {
      case 'attempt':
        localizeAttempt.value = e.data.attempt
        clearVariants()
        break
      case 'delta': {
        const target = (variants[e.data.language] ??= emptyContent())
        setField(target, e.data.field, getField(target, e.data.field) + e.data.text)
        break
      }
      case 'rejected':
        localizeRejections.value.push({ attempt: e.data.attempt, errors: e.data.errors })
        break
      case 'result':
        // Only this event carries validated texts; it replaces the preview.
        // Copies, so editing a text never writes into the event's own objects.
        clearVariants()
        for (const [code, text] of Object.entries(e.data.variants)) variants[code] = copyContent(text)
        localizedFrom.value = JSON.stringify(source)
        break
      case 'error':
        clearVariants()
        localizeError.value = { message: e.data.message, details: e.data.details ?? [] }
        break
    }
  }

  /**
   * Writes the texts for every other language of the audience from the main
   * text as it stands now, edits included. One request for all of them.
   */
  async function localize() {
    const to = languages.value.map((l) => l.language).filter((code) => code !== contentLanguage.value)
    if (to.length === 0 || contentMeta.value === null || streaming.value) return
    clearLocalization()
    const controller = new AbortController()
    abortLocalize = controller
    localizing.value = true
    approveError.value = null
    const source = copyContent(content)
    activeLanguage.value = to[0] ?? contentLanguage.value // watch the first one being written
    try {
      await api.streamLocalize(brief.value, source, contentLanguage.value, to, (e) => onLocalizeEvent(e, source), controller.signal)
      if (localizedFrom.value === null && !localizeError.value) {
        clearVariants()
        localizeError.value = { message: 'Akış sonuç gelmeden kesildi', details: [] }
      }
    } catch (err) {
      if (controller.signal.aborted) return // superseded
      clearVariants()
      localizeError.value = describe(err)
    } finally {
      if (abortLocalize === controller) {
        localizing.value = false
        if (!hasText(activeLanguage.value)) activeLanguage.value = contentLanguage.value
      }
    }
  }

  // --- measuring ------------------------------------------------------------------
  function measureContent(c: Content) {
    const variables = contentMeta.value?.variables ?? {}
    return {
      sms: measure(c.sms, variables),
      subject: measure(c.email.subject, variables),
      body: measure(c.email.body, variables),
    }
  }

  /**
   * Live measurement of the limited fields of the language being edited,
   * with the same rules the backend applies on approval.
   */
  const measured = computed(() => measureContent(activeContent.value))

  const limits = computed<Lengths>(() => contentMeta.value?.limits ?? { sms: 160, email_subject: 80 })

  function withinLimits(c: Content): boolean {
    const m = measureContent(c)
    return (
      m.sms.problems.length === 0 &&
      m.subject.problems.length === 0 &&
      m.body.problems.length === 0 &&
      m.sms.length <= limits.value.sms &&
      m.subject.length <= limits.value.email_subject
    )
  }

  /** Languages whose text would be refused: every one of them is checked, not only the visible one. */
  const invalidLanguages = computed(() => {
    const bad: string[] = []
    if (!withinLimits(content)) bad.push(contentLanguage.value)
    for (const [code, text] of Object.entries(variants)) if (!withinLimits(text)) bad.push(code)
    return bad
  })

  const contentValid = computed(() => contentMeta.value !== null && !localizing.value && invalidLanguages.value.length === 0)

  // --- approval and delivery ----------------------------------------------------
  // "now": send at once. "later": start at scheduledFor, a local date-time as
  // an <input type="datetime-local"> holds it ("2026-11-14T09:30").
  const sendMode = ref<'now' | 'later'>('now')
  const scheduledFor = ref('')

  // A send time the model read in the brief. It only pre-fills the two
  // fields above; the marketer still sees it, can change it, and approves.
  const scheduleSuggestion = ref<{ value: string; phrase: string; interpretation: string } | null>(null)

  function applyScheduleSuggestion(schedule: SegmentResponse['schedule']) {
    const at = schedule ? new Date(schedule.send_at) : null
    if (!schedule || !at || Number.isNaN(at.getTime())) {
      // A new brief without a send time starts from "send now" again.
      scheduleSuggestion.value = null
      sendMode.value = 'now'
      scheduledFor.value = ''
      return
    }
    const value = toLocalInput(at)
    scheduleSuggestion.value = { value, phrase: schedule.phrase, interpretation: schedule.interpretation }
    sendMode.value = 'later'
    scheduledFor.value = value
  }

  /** The chosen start as an instant, or null when it is missing or not a date. */
  const scheduledAt = computed<Date | null>(() => (sendMode.value === 'later' ? fromLocalInput(scheduledFor.value) : null))

  /** Why the schedule cannot be used, or null when it can. */
  const scheduleProblem = computed<string | null>(() => {
    if (sendMode.value !== 'later') return null
    if (!scheduledAt.value) return 'Gönderim tarihini ve saatini seç'
    if (scheduledAt.value.getTime() <= Date.now()) return 'Gönderim zamanı geçmişte olamaz'
    return null
  })

  const approving = ref(false)
  const approveError = ref<{ message: string; details: FieldError[] } | null>(null)
  // The campaign on the dashboard: just created, or opened from the list.
  const campaign = ref<Pick<CampaignCreated, 'id'> | null>(null)
  const status = ref<CampaignStatus | null>(null)
  const done = ref(false)
  const statusError = ref<string | null>(null)
  let stopWatching: (() => void) | null = null

  async function approve() {
    const rule = segment.value?.rule
    if (!rule || !languagesResolved.value) return
    approving.value = true
    approveError.value = null
    try {
      // Whatever the counters above say, the backend validates the rule and
      // the edited texts again here and may still refuse.
      const texts: LocalizedContent = { default: contentLanguage.value, variants: { [contentLanguage.value]: copyContent(content) } }
      for (const [code, text] of Object.entries(variants)) texts.variants[code] = copyContent(text)
      campaign.value = await api.createCampaign(brief.value, rule, texts, scheduledAt.value?.toISOString())
      standalone.value = false
      step.value = 'dashboard'
      watch(campaign.value.id)
    } catch (err) {
      approveError.value = describe(err)
    } finally {
      approving.value = false
    }
  }

  // --- recipients of the campaign on the dashboard ---------------------------------
  // The people behind the numbers, in a panel the marketer opens on demand:
  // nothing is fetched until then. A page at a time; the list is a snapshot
  // (the counts on the dashboard are live), reloaded when the campaign
  // finishes while the panel is open.
  const recipientsOpen = ref(false)
  const recipients = ref<Recipient[]>([])
  const recipientsTotal = ref(0)
  const recipientsFilter = ref<MessageStatus | ''>('')
  const recipientsLoading = ref(false)
  const recipientsError = ref<string | null>(null)
  // Only the answer to the latest request is used: a slow page for a filter
  // the marketer has already left must not overwrite the current one.
  let recipientsRequest = 0

  function clearRecipients() {
    recipientsRequest++
    recipientsOpen.value = false
    recipients.value = []
    recipientsTotal.value = 0
    recipientsFilter.value = ''
    recipientsLoading.value = false
    recipientsError.value = null
  }

  /** Loads the first page, or with more = true the page after what is shown. */
  async function loadRecipients(more = false) {
    const id = campaign.value?.id
    if (id === undefined) return
    const request = ++recipientsRequest
    recipientsLoading.value = true
    recipientsError.value = null
    try {
      const page = await api.listRecipients(id, recipientsFilter.value, more ? recipients.value.length : 0)
      if (request !== recipientsRequest) return
      recipients.value = more ? [...recipients.value, ...page.recipients] : page.recipients
      recipientsTotal.value = page.total
    } catch (err) {
      if (request !== recipientsRequest) return
      recipientsError.value = describe(err).message
    } finally {
      if (request === recipientsRequest) recipientsLoading.value = false
    }
  }

  function filterRecipients(status: MessageStatus | '') {
    recipientsFilter.value = status
    return loadRecipients()
  }

  /** Opens the panel, optionally on one status, and loads its first page. */
  function openRecipients(status: MessageStatus | '' = '') {
    recipientsOpen.value = true
    return filterRecipients(status)
  }

  function closeRecipients() {
    recipientsOpen.value = false
  }

  function watch(id: number) {
    stopWatching?.()
    status.value = null
    done.value = false
    statusError.value = null
    clearRecipients()
    stopWatching = api.watchCampaign(id, {
      onStatus: (s) => (status.value = s),
      onDone: () => {
        done.value = true
        if (recipientsOpen.value) void loadRecipients() // the final outcome of every message
      },
      onError: (message) => (statusError.value = message),
    })
  }

  const cancelling = ref(false)

  /** Stops the campaign: nothing that is still waiting will be sent. */
  async function cancelCampaign() {
    if (!campaign.value) return
    cancelling.value = true
    statusError.value = null
    try {
      // The live stream reports the same thing a moment later; showing the
      // answer at once keeps the button from looking ignored.
      status.value = await api.cancelCampaign(campaign.value.id)
      if (recipientsOpen.value) void loadRecipients()
    } catch (err) {
      statusError.value = describe(err).message
    } finally {
      cancelling.value = false
    }
  }

  // --- campaign list -------------------------------------------------------------
  const campaigns = ref<CampaignSummary[] | null>(null)
  const campaignsError = ref<string | null>(null)

  async function loadCampaigns() {
    try {
      campaigns.value = await api.listCampaigns()
      campaignsError.value = null
    } catch (err) {
      campaignsError.value = describe(err).message
    }
  }

  /** Shows the home page. Work in progress in the flow is kept. */
  function showHome() {
    view.value = 'home'
  }

  /** Shows the list. Work in progress in the flow is kept, not thrown away. */
  function showCampaigns() {
    view.value = 'campaigns'
    void loadCampaigns()
  }

  /** Opens the dashboard of an existing campaign and follows it live. */
  function openCampaign(id: number) {
    abortContent?.abort()
    abortLocalize?.abort()
    streaming.value = false
    localizing.value = false
    view.value = 'compose'
    standalone.value = true
    step.value = 'dashboard'
    campaign.value = { id }
    watch(id)
  }

  /** Where the app is, for the URL hash (see lib/route.ts). */
  const route = computed<Route>(() => {
    if (view.value === 'home') return { name: 'home' }
    if (view.value === 'campaigns') return { name: 'campaigns' }
    if (step.value === 'dashboard' && campaign.value) return { name: 'campaign', id: campaign.value.id }
    return { name: 'compose' }
  })

  /** Goes where the URL hash says (page load, back and forward buttons). */
  function navigate(to: Route) {
    switch (to.name) {
      case 'home':
        showHome()
        break
      case 'campaigns':
        showCampaigns()
        break
      case 'campaign':
        if (step.value === 'dashboard' && campaign.value?.id === to.id) view.value = 'compose'
        else openCampaign(to.id)
        break
      case 'compose':
        // Back from a dashboard means a fresh start; back from the list
        // returns to whatever was being composed.
        if (step.value === 'dashboard') reset()
        else view.value = 'compose'
        break
    }
  }

  function reset() {
    abortContent?.abort()
    stopWatching?.()
    stopWatching = null
    view.value = 'compose'
    standalone.value = false
    step.value = 'brief'
    brief.value = ''
    segment.value = null
    segmentError.value = null
    clearContent()
    clearLocalization()
    contentLanguage.value = DEFAULT_LANGUAGE
    activeLanguage.value = DEFAULT_LANGUAGE
    streaming.value = false
    attempt.value = 0
    rejections.value = []
    contentMeta.value = null
    contentError.value = null
    approveError.value = null
    sendMode.value = 'now'
    scheduledFor.value = ''
    scheduleSuggestion.value = null
    campaign.value = null
    status.value = null
    done.value = false
    statusError.value = null
    clearRecipients()
  }

  return {
    step, brief,
    segment, segmentLoading, segmentError, generateSegment, recountSegment,
    ruleOptions, loadRuleOptions, applyRule,
    content, streaming, attempt, rejections, contentMeta, contentError, generateContent,
    languages, languageLabel, contentLanguage, activeLanguage, activeContent, hasText,
    variants, localizing, localizeAttempt, localizeRejections, localizeError, localize,
    missingLanguages, translationsStale, invalidLanguages, languagesResolved,
    measured, limits, contentValid,
    sendMode, scheduledFor, scheduleProblem, scheduleSuggestion,
    approving, approveError, campaign, status, done, statusError, approve,
    cancelling, cancelCampaign,
    recipients, recipientsTotal, recipientsFilter, recipientsLoading, recipientsError, loadRecipients, filterRecipients,
    recipientsOpen, openRecipients, closeRecipients,
    view, standalone, campaigns, campaignsError, loadCampaigns, showHome, showCampaigns, openCampaign, route, navigate,
    reset,
  }
})
