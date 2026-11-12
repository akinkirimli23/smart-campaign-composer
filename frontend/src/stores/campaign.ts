import { computed, reactive, ref } from 'vue'
import { defineStore } from 'pinia'
import { api, ApiError } from '../api/client'
import { measure } from '../lib/template'
import type {
  CampaignCreated,
  CampaignStatus,
  Content,
  ContentEvent,
  ContentField,
  ContentResult,
  FieldError,
  Lengths,
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

function emptyContent(): Content {
  return { sms: '', email: { subject: '', body: '' } }
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

  // --- audience ---------------------------------------------------------------
  const segment = ref<SegmentResponse | null>(null)
  const segmentLoading = ref(false)
  const segmentError = ref<{ message: string; details: FieldError[] } | null>(null)

  async function generateSegment() {
    segmentLoading.value = true
    segmentError.value = null
    try {
      segment.value = await api.generateSegment(brief.value)
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
  const content = reactive<Content>(emptyContent())
  const streaming = ref(false)
  const attempt = ref(0)
  const rejections = ref<Rejection[]>([])
  const contentMeta = ref<Omit<ContentResult, 'content'> | null>(null)
  const contentError = ref<{ message: string; details: FieldError[] } | null>(null)
  let abortContent: AbortController | null = null

  function setField(field: ContentField, value: string) {
    if (field === 'sms') content.sms = value
    else if (field === 'email.subject') content.email.subject = value
    else content.email.body = value
  }

  function getField(field: ContentField): string {
    if (field === 'sms') return content.sms
    return field === 'email.subject' ? content.email.subject : content.email.body
  }

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
        setField(e.data.field, getField(e.data.field) + e.data.text)
        break
      case 'rejected':
        rejections.value.push({ attempt: e.data.attempt, errors: e.data.errors })
        break
      case 'result': {
        // Only this event carries validated content; it replaces the preview.
        const { content: validated, ...meta } = e.data
        Object.assign(content, validated)
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
    try {
      await api.streamContent(brief.value, onContentEvent, controller.signal)
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

  /**
   * Live measurement of the limited fields as the marketer edits them, with
   * the same rules the backend applies on approval.
   */
  const measured = computed(() => {
    const variables = contentMeta.value?.variables ?? {}
    return {
      sms: measure(content.sms, variables),
      subject: measure(content.email.subject, variables),
      body: measure(content.email.body, variables),
    }
  })

  const limits = computed<Lengths>(() => contentMeta.value?.limits ?? { sms: 160, email_subject: 80 })

  const contentValid = computed(() => {
    const m = measured.value
    return (
      contentMeta.value !== null &&
      m.sms.problems.length === 0 &&
      m.subject.problems.length === 0 &&
      m.body.problems.length === 0 &&
      m.sms.length <= limits.value.sms &&
      m.subject.length <= limits.value.email_subject
    )
  })

  // --- approval and delivery ----------------------------------------------------
  // "now": send at once. "later": start at scheduledFor, a local date-time as
  // an <input type="datetime-local"> holds it ("2026-11-14T09:30").
  const sendMode = ref<'now' | 'later'>('now')
  const scheduledFor = ref('')

  /** The chosen start as an instant, or null when it is missing or not a date. */
  const scheduledAt = computed<Date | null>(() => {
    if (sendMode.value !== 'later' || scheduledFor.value === '') return null
    const at = new Date(scheduledFor.value) // parsed in the marketer's time zone
    return Number.isNaN(at.getTime()) ? null : at
  })

  /** Why the schedule cannot be used, or null when it can. */
  const scheduleProblem = computed<string | null>(() => {
    if (sendMode.value !== 'later') return null
    if (!scheduledAt.value) return 'Gönderim tarihini ve saatini seç'
    if (scheduledAt.value.getTime() <= Date.now()) return 'Gönderim zamanı geçmişte olamaz'
    return null
  })

  const approving = ref(false)
  const approveError = ref<{ message: string; details: FieldError[] } | null>(null)
  const campaign = ref<CampaignCreated | null>(null)
  const status = ref<CampaignStatus | null>(null)
  const done = ref(false)
  const statusError = ref<string | null>(null)
  let stopWatching: (() => void) | null = null

  async function approve() {
    const rule = segment.value?.rule
    if (!rule) return
    approving.value = true
    approveError.value = null
    try {
      // Whatever the counters above say, the backend validates the rule and
      // the edited texts again here and may still refuse.
      campaign.value = await api.createCampaign(
        brief.value,
        rule,
        { sms: content.sms, email: { subject: content.email.subject, body: content.email.body } },
        scheduledAt.value?.toISOString(),
      )
      step.value = 'dashboard'
      watch(campaign.value.id)
    } catch (err) {
      approveError.value = describe(err)
    } finally {
      approving.value = false
    }
  }

  function watch(id: number) {
    stopWatching?.()
    status.value = null
    done.value = false
    statusError.value = null
    stopWatching = api.watchCampaign(id, {
      onStatus: (s) => (status.value = s),
      onDone: () => (done.value = true),
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
    } catch (err) {
      statusError.value = describe(err).message
    } finally {
      cancelling.value = false
    }
  }

  function reset() {
    abortContent?.abort()
    stopWatching?.()
    stopWatching = null
    step.value = 'brief'
    brief.value = ''
    segment.value = null
    segmentError.value = null
    clearContent()
    streaming.value = false
    attempt.value = 0
    rejections.value = []
    contentMeta.value = null
    contentError.value = null
    approveError.value = null
    sendMode.value = 'now'
    scheduledFor.value = ''
    campaign.value = null
    status.value = null
    done.value = false
    statusError.value = null
  }

  return {
    step, brief,
    segment, segmentLoading, segmentError, generateSegment, recountSegment,
    ruleOptions, loadRuleOptions, applyRule,
    content, streaming, attempt, rejections, contentMeta, contentError, generateContent,
    measured, limits, contentValid,
    sendMode, scheduledFor, scheduleProblem,
    approving, approveError, campaign, status, done, statusError, approve,
    cancelling, cancelCampaign,
    reset,
  }
})
