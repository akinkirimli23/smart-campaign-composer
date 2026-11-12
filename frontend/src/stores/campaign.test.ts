import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { CampaignStatus, Content, ContentEvent, ContentResult, LocalizeEvent, SegmentResponse } from '../api/types'

// The store is tested against a fake API: no network, no backend.
const fake = vi.hoisted(() => ({
  generateSegment: vi.fn(),
  countSegment: vi.fn(),
  segmentOptions: vi.fn(),
  createCampaign: vi.fn(),
  cancelCampaign: vi.fn(),
  listCampaigns: vi.fn(),
  streamContent: vi.fn(),
  streamLocalize: vi.fn(),
  watchCampaign: vi.fn(),
}))

vi.mock('../api/client', async (original) => ({
  ...(await original<typeof import('../api/client')>()),
  api: fake,
}))

import { ApiError } from '../api/client'
import { useCampaignStore } from './campaign'

const segment: SegmentResponse = {
  status: 'ok',
  rule: { match: 'all', conditions: [{ type: 'attribute', field: 'city', op: 'in', values: ['İstanbul'] }] },
  description: { summary: 'Aşağıdaki koşulu sağlayan kullanıcılar', conditions: ['Şehir: İstanbul'] },
  assumptions: [],
  audience: { total: 381, sms: 222, email: 254, unreachable: 58 },
}

const result: ContentResult = {
  content: { sms: 'Merhaba {{first_name}}, %30 indirim!', email: { subject: 'Fırsat', body: 'Merhaba' } },
  lengths: { sms: 30, email_subject: 6 },
  limits: { sms: 160, email_subject: 80 },
  variables: { first_name: 10, last_name: 12, city: 9 },
  attempts: 1,
}

/** Makes streamContent replay the given events. */
function script(...events: ContentEvent[]) {
  fake.streamContent.mockImplementation(async (_brief: string, _language: string, onEvent: (e: ContentEvent) => void) => {
    for (const e of events) onEvent(e)
  })
}

async function storeAtContent() {
  const store = useCampaignStore()
  store.brief = 'brief'
  fake.generateSegment.mockResolvedValue(segment)
  await store.generateSegment()
  script({ event: 'attempt', data: { attempt: 1 } }, { event: 'result', data: result })
  await store.generateContent()
  return store
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.resetAllMocks()
})

describe('audience', () => {
  it('moves to the audience step with the proposed rule', async () => {
    const store = useCampaignStore()
    store.brief = 'brief'
    fake.generateSegment.mockResolvedValue(segment)
    await store.generateSegment()
    expect(fake.generateSegment).toHaveBeenCalledWith('brief')
    expect(store.step).toBe('audience')
    expect(store.segment?.audience?.sms).toBe(222)
  })

  it('stays on the brief and shows the error when the request fails', async () => {
    const store = useCampaignStore()
    fake.generateSegment.mockRejectedValue(new ApiError(422, 'llm_output_invalid', 'Model geçerli bir kural üretemedi', [{ path: 'conditions[0]', message: 'x' }]))
    await store.generateSegment()
    expect(store.step).toBe('brief')
    expect(store.segmentError).toEqual({ message: 'Model geçerli bir kural üretemedi', details: [{ path: 'conditions[0]', message: 'x' }] })
    expect(store.segmentLoading).toBe(false)
  })

  it('does not call the backend with an edited rule that is not JSON', async () => {
    const store = useCampaignStore()
    await store.recountSegment('{not json')
    expect(fake.countSegment).not.toHaveBeenCalled()
    expect(store.segmentError?.message).toContain('JSON')
  })

  it('keeps the previous audience when the edited rule is rejected', async () => {
    const store = useCampaignStore()
    fake.generateSegment.mockResolvedValue(segment)
    await store.generateSegment()
    fake.countSegment.mockRejectedValue(new ApiError(422, 'rule_invalid', 'Kitle kuralı geçersiz', [{ path: 'conditions[0].values[0]', message: 'geçerli bir şehir değil' }]))
    await store.recountSegment('{"match":"all","conditions":[]}')
    expect(store.segment).toEqual(segment)
    expect(store.segmentError?.details[0]?.path).toBe('conditions[0].values[0]')
  })
})

describe('rule editor', () => {
  const edited = { match: 'all', conditions: [{ type: 'signup', op: 'within', days: 7 }] }

  it('replaces the audience when the backend accepts the edited rule', async () => {
    const store = useCampaignStore()
    fake.generateSegment.mockResolvedValue(segment)
    await store.generateSegment()
    const recounted: SegmentResponse = { ...segment, audience: { total: 20, sms: 9, email: 15, unreachable: 3 }, assumptions: undefined }
    fake.countSegment.mockResolvedValue(recounted)

    expect(await store.applyRule(edited)).toBe(true)
    expect(fake.countSegment).toHaveBeenCalledWith(edited)
    expect(store.segment?.audience?.total).toBe(20)
    expect(store.segmentError).toBeNull()
  })

  it('keeps the previous rule and reports the errors when the backend refuses', async () => {
    const store = useCampaignStore()
    fake.generateSegment.mockResolvedValue(segment)
    await store.generateSegment()
    fake.countSegment.mockRejectedValue(new ApiError(422, 'rule_invalid', 'Kitle kuralı geçersiz', [{ path: 'conditions[0].days', message: '0 geçersiz' }]))

    expect(await store.applyRule(edited)).toBe(false)
    expect(store.segment).toEqual(segment)
    expect(store.segmentError?.details).toEqual([{ path: 'conditions[0].days', message: '0 geçersiz' }])
  })

  it('loads the options once', async () => {
    const store = useCampaignStore()
    fake.segmentOptions.mockResolvedValue({ cities: [] })
    await store.loadRuleOptions()
    await store.loadRuleOptions()
    expect(fake.segmentOptions).toHaveBeenCalledTimes(1)
    expect(store.ruleOptions).toEqual({ cities: [] })
  })
})

describe('content stream', () => {
  it('builds the fields from deltas and replaces them with the validated result', async () => {
    const store = useCampaignStore()
    const seen: string[] = []
    fake.streamContent.mockImplementation(async (_b: string, _l: string, onEvent: (e: ContentEvent) => void) => {
      onEvent({ event: 'attempt', data: { attempt: 1 } })
      onEvent({ event: 'delta', data: { field: 'sms', text: 'Merhaba {{fir' } })
      onEvent({ event: 'delta', data: { field: 'sms', text: 'st_name}}' } })
      onEvent({ event: 'delta', data: { field: 'email.subject', text: 'Fır' } })
      seen.push(store.content.sms, store.content.email.subject, String(store.streaming))
      onEvent({ event: 'result', data: result })
    })
    await store.generateContent()

    expect(seen).toEqual(['Merhaba {{first_name}}', 'Fır', 'true'])
    expect(store.step).toBe('content')
    expect(store.content).toEqual(result.content)
    expect(store.streaming).toBe(false)
    expect(store.contentValid).toBe(true)
  })

  it('clears the text when a new attempt starts and keeps the rejection visible', async () => {
    const store = useCampaignStore()
    const errors = [{ path: 'sms', message: '{{name}} bilinmeyen bir değişken' }]
    let afterSecondAttempt = 'unset'
    fake.streamContent.mockImplementation(async (_b: string, _l: string, onEvent: (e: ContentEvent) => void) => {
      onEvent({ event: 'attempt', data: { attempt: 1 } })
      onEvent({ event: 'delta', data: { field: 'sms', text: 'Merhaba {{name}}' } })
      onEvent({ event: 'rejected', data: { attempt: 1, errors } })
      onEvent({ event: 'attempt', data: { attempt: 2 } })
      afterSecondAttempt = store.content.sms
      onEvent({ event: 'result', data: { ...result, attempts: 2 } })
    })
    await store.generateContent()

    expect(afterSecondAttempt).toBe('')
    expect(store.attempt).toBe(2)
    expect(store.rejections).toEqual([{ attempt: 1, errors }])
    expect(store.content.sms).toBe(result.content.sms)
  })

  it('shows no half-written text when generation fails', async () => {
    const store = useCampaignStore()
    script(
      { event: 'attempt', data: { attempt: 1 } },
      { event: 'delta', data: { field: 'sms', text: 'yarım' } },
      { event: 'error', data: { code: 'llm_output_invalid', message: 'Model limitlere uyan bir içerik üretemedi', details: [{ path: 'sms', message: 'x' }] } },
    )
    await store.generateContent()
    expect(store.content.sms).toBe('')
    expect(store.contentError?.message).toContain('üretemedi')
    expect(store.contentValid).toBe(false)
  })

  it('reports a stream that ends without a result', async () => {
    const store = useCampaignStore()
    script({ event: 'attempt', data: { attempt: 1 } })
    await store.generateContent()
    expect(store.contentError).not.toBeNull()
    expect(store.contentValid).toBe(false)
  })
})

describe('editing', () => {
  it('measures edits live and blocks approval over the limit', async () => {
    const store = await storeAtContent()
    expect(store.measured.sms.length).toBe(32) // 22 literal characters + first_name counted as 10

    store.content.sms = 'x'.repeat(151) + '{{first_name}}'
    expect(store.measured.sms.length).toBe(161)
    expect(store.contentValid).toBe(false)

    store.content.sms = 'x'.repeat(150) + '{{first_name}}'
    expect(store.measured.sms.length).toBe(160)
    expect(store.contentValid).toBe(true)
  })

  it('blocks approval on an unknown variable and on a long subject', async () => {
    const store = await storeAtContent()
    store.content.sms = 'Merhaba {{nickname}}'
    expect(store.contentValid).toBe(false)

    store.content.sms = 'Merhaba'
    store.content.email.subject = 'ş'.repeat(81)
    expect(store.contentValid).toBe(false)
    store.content.email.subject = 'ş'.repeat(80)
    expect(store.contentValid).toBe(true)
  })
})

describe('approval and delivery', () => {
  it('sends the edited texts and the rule, then follows the campaign live', async () => {
    const store = await storeAtContent()
    store.content.sms = 'Elle düzenlendi {{first_name}}'
    fake.createCampaign.mockResolvedValue({ id: 7, as_of: 'now', send_at: 'now', queued: { sms: 222, email: 254 } })
    let handlers: { onStatus: (s: CampaignStatus) => void; onDone: () => void } | undefined
    fake.watchCampaign.mockImplementation((_id: number, h: typeof handlers) => {
      handlers = h
      return () => {}
    })

    await store.approve()

    expect(fake.createCampaign).toHaveBeenCalledWith(
      'brief',
      segment.rule,
      // No language information on this audience: one text, in the default language.
      { default: 'tr', variants: { tr: { sms: 'Elle düzenlendi {{first_name}}', email: result.content.email } } },
      undefined, // no schedule: send at once
    )
    expect(store.step).toBe('dashboard')
    expect(fake.watchCampaign).toHaveBeenCalledWith(7, expect.anything())

    const status: CampaignStatus = {
      id: 7,
      brief: 'brief',
      created_at: 'now',
      send_at: 'now',
      done: false,
      channels: {
        sms: { total: 222, pending: 100, sending: 22, sent: 100, failed: 0, skipped: 0, cancelled: 0 },
        email: { total: 254, pending: 254, sending: 0, sent: 0, failed: 0, skipped: 0, cancelled: 0 },
      },
    }
    handlers?.onStatus(status)
    expect(store.status?.channels.sms.sent).toBe(100)
    expect(store.done).toBe(false)
    handlers?.onDone()
    expect(store.done).toBe(true)
  })

  it('stays on the content step and shows the server errors when approval is refused', async () => {
    const store = await storeAtContent()
    // The browser's counter may be wrong or bypassed; the backend has the last word.
    fake.createCampaign.mockRejectedValue(new ApiError(422, 'content_invalid', 'İçerik geçersiz', [{ path: 'sms', message: '161 karakter, limit 160' }]))
    await store.approve()
    expect(store.step).toBe('content')
    expect(store.approveError?.details).toEqual([{ path: 'sms', message: '161 karakter, limit 160' }])
    expect(fake.watchCampaign).not.toHaveBeenCalled()
  })

  it('reset stops watching and returns to an empty brief', async () => {
    const store = await storeAtContent()
    const stop = vi.fn()
    fake.createCampaign.mockResolvedValue({ id: 7, as_of: 'now', send_at: 'now', queued: { sms: 1, email: 1 } })
    fake.watchCampaign.mockReturnValue(stop)
    await store.approve()
    store.reset()
    expect(stop).toHaveBeenCalled()
    expect(store.step).toBe('brief')
    expect(store.brief).toBe('')
    expect(store.content.sms).toBe('')
    expect(store.campaign).toBeNull()
  })
})

describe('scheduling and cancelling', () => {
  const created = { id: 7, as_of: 'now', send_at: 'later', queued: { sms: 222, email: 254 } }

  it('sends the chosen start time as an instant', async () => {
    const store = await storeAtContent()
    fake.createCampaign.mockResolvedValue(created)
    fake.watchCampaign.mockReturnValue(() => {})
    const start = new Date(Date.now() + 3 * 24 * 3600 * 1000)
    start.setSeconds(0, 0)
    const pad = (n: number) => String(n).padStart(2, '0')
    // What <input type="datetime-local"> holds: local time without a zone.
    store.sendMode = 'later'
    store.scheduledFor = `${start.getFullYear()}-${pad(start.getMonth() + 1)}-${pad(start.getDate())}T${pad(start.getHours())}:${pad(start.getMinutes())}`

    expect(store.scheduleProblem).toBeNull()
    await store.approve()
    expect(fake.createCampaign.mock.calls[0]?.[3]).toBe(start.toISOString())
    expect(store.step).toBe('dashboard')
  })

  it('reports a missing or past time and never sends it', async () => {
    const store = await storeAtContent()
    store.sendMode = 'later'
    expect(store.scheduleProblem).toContain('seç')

    store.scheduledFor = '2020-01-01T09:00'
    expect(store.scheduleProblem).toContain('geçmişte')

    store.sendMode = 'now' // switching back makes the stale date irrelevant
    expect(store.scheduleProblem).toBeNull()
    fake.createCampaign.mockResolvedValue(created)
    fake.watchCampaign.mockReturnValue(() => {})
    await store.approve()
    expect(fake.createCampaign.mock.calls[0]?.[3]).toBeUndefined()
  })

  it('shows the cancelled state as soon as the backend confirms it', async () => {
    const store = await storeAtContent()
    fake.createCampaign.mockResolvedValue(created)
    fake.watchCampaign.mockReturnValue(() => {})
    await store.approve()

    const cancelled: CampaignStatus = {
      id: 7,
      brief: 'brief',
      created_at: 'now',
      send_at: 'later',
      cancelled_at: 'now',
      done: true,
      channels: {
        sms: { total: 222, pending: 0, sending: 0, sent: 0, failed: 0, skipped: 0, cancelled: 222 },
        email: { total: 254, pending: 0, sending: 0, sent: 0, failed: 0, skipped: 0, cancelled: 254 },
      },
    }
    fake.cancelCampaign.mockResolvedValue(cancelled)
    await store.cancelCampaign()
    expect(fake.cancelCampaign).toHaveBeenCalledWith(7)
    expect(store.status?.cancelled_at).toBe('now')
    expect(store.cancelling).toBe(false)
  })

  it('keeps the dashboard and shows the error when cancelling fails', async () => {
    const store = await storeAtContent()
    fake.createCampaign.mockResolvedValue(created)
    fake.watchCampaign.mockReturnValue(() => {})
    await store.approve()
    fake.cancelCampaign.mockRejectedValue(new ApiError(500, 'internal', 'Kampanya iptal edilemedi'))
    await store.cancelCampaign()
    expect(store.statusError).toBe('Kampanya iptal edilemedi')
    expect(store.step).toBe('dashboard')
  })
})

describe('send time read from the brief', () => {
  const sendAt = new Date(Date.now() + 2 * 24 * 3600 * 1000)
  sendAt.setSeconds(0, 0)
  const suggested: SegmentResponse = {
    ...segment,
    schedule: { send_at: sendAt.toISOString(), phrase: 'cuma sabahı', interpretation: 'Cuma 09:00 olarak yorumlandı.' },
  }

  it('pre-fills the schedule and leaves the decision to the marketer', async () => {
    const store = useCampaignStore()
    store.brief = 'brief'
    fake.generateSegment.mockResolvedValue(suggested)
    await store.generateSegment()

    expect(store.sendMode).toBe('later')
    expect(store.scheduleSuggestion?.phrase).toBe('cuma sabahı')
    expect(new Date(store.scheduledFor).getTime()).toBe(sendAt.getTime())
    expect(store.scheduleProblem).toBeNull()
    expect(fake.createCampaign).not.toHaveBeenCalled() // nothing is scheduled without approval

    // Approving as suggested sends exactly that instant.
    script({ event: 'attempt', data: { attempt: 1 } }, { event: 'result', data: result })
    await store.generateContent()
    fake.createCampaign.mockResolvedValue({ id: 7, as_of: 'now', send_at: 'later', queued: { sms: 1, email: 1 } })
    fake.watchCampaign.mockReturnValue(() => {})
    await store.approve()
    expect(fake.createCampaign.mock.calls[0]?.[3]).toBe(sendAt.toISOString())
  })

  it('can be overridden: choosing "now" sends at once', async () => {
    const store = useCampaignStore()
    fake.generateSegment.mockResolvedValue(suggested)
    await store.generateSegment()
    store.sendMode = 'now'
    script({ event: 'attempt', data: { attempt: 1 } }, { event: 'result', data: result })
    await store.generateContent()
    fake.createCampaign.mockResolvedValue({ id: 7, as_of: 'now', send_at: 'now', queued: { sms: 1, email: 1 } })
    fake.watchCampaign.mockReturnValue(() => {})
    await store.approve()
    expect(fake.createCampaign.mock.calls[0]?.[3]).toBeUndefined()
  })

  it('a new brief without a send time goes back to "send now"', async () => {
    const store = useCampaignStore()
    fake.generateSegment.mockResolvedValue(suggested)
    await store.generateSegment()
    fake.generateSegment.mockResolvedValue(segment)
    await store.generateSegment()
    expect(store.sendMode).toBe('now')
    expect(store.scheduledFor).toBe('')
    expect(store.scheduleSuggestion).toBeNull()
  })

  it('editing the rule keeps the suggestion', async () => {
    const store = useCampaignStore()
    fake.generateSegment.mockResolvedValue(suggested)
    await store.generateSegment()
    fake.countSegment.mockResolvedValue(segment) // the recount carries no schedule
    await store.applyRule({ match: 'all', conditions: [] })
    expect(store.sendMode).toBe('later')
    expect(store.scheduleSuggestion?.phrase).toBe('cuma sabahı')
  })
})

describe('campaign list and navigation', () => {
  const list = [
    { id: 9, brief: 'haftaya', created_at: 'x', send_at: 'x', state: 'scheduled', messages: { total: 18, pending: 18, sending: 0, sent: 0, failed: 0, skipped: 0, cancelled: 0 } },
  ]

  it('loads the list without throwing away a campaign being composed', async () => {
    const store = await storeAtContent()
    fake.listCampaigns.mockResolvedValue(list)
    store.showCampaigns()
    await Promise.resolve()

    expect(store.view).toBe('campaigns')
    expect(store.route).toEqual({ name: 'campaigns' })
    expect(store.campaigns).toEqual(list)
    expect(store.content.sms).toBe(result.content.sms) // the draft is still there

    store.navigate({ name: 'compose' }) // browser back
    expect(store.view).toBe('compose')
    expect(store.step).toBe('content')
  })

  it('keeps the previous list and reports the error when a refresh fails', async () => {
    const store = useCampaignStore()
    fake.listCampaigns.mockResolvedValue(list)
    await store.loadCampaigns()
    fake.listCampaigns.mockRejectedValue(new ApiError(500, 'internal', 'Kampanyalar okunamadı'))
    await store.loadCampaigns()
    expect(store.campaigns).toEqual(list)
    expect(store.campaignsError).toBe('Kampanyalar okunamadı')
  })

  it('opens an existing campaign on the dashboard and follows it', () => {
    const store = useCampaignStore()
    fake.watchCampaign.mockReturnValue(() => {})
    store.openCampaign(9)
    expect(store.step).toBe('dashboard')
    expect(store.standalone).toBe(true)
    expect(store.route).toEqual({ name: 'campaign', id: 9 })
    expect(fake.watchCampaign).toHaveBeenCalledWith(9, expect.anything())
  })

  it('a campaign just approved is addressable too, so a reload comes back to it', async () => {
    const store = await storeAtContent()
    fake.createCampaign.mockResolvedValue({ id: 7, as_of: 'now', send_at: 'now', queued: { sms: 1, email: 1 } })
    fake.watchCampaign.mockReturnValue(() => {})
    await store.approve()
    expect(store.route).toEqual({ name: 'campaign', id: 7 })
    expect(store.standalone).toBe(false)
  })

  it('follows the address: a campaign link, back to the list, back to an empty composer', () => {
    const store = useCampaignStore()
    const stop = vi.fn()
    fake.watchCampaign.mockReturnValue(stop)
    fake.listCampaigns.mockResolvedValue(list)

    store.navigate({ name: 'campaign', id: 9 }) // page opened at #/kampanya/9
    expect(store.route).toEqual({ name: 'campaign', id: 9 })

    store.navigate({ name: 'campaigns' })
    expect(store.view).toBe('campaigns')
    store.navigate({ name: 'campaign', id: 9 }) // back: the same campaign is not re-opened
    expect(fake.watchCampaign).toHaveBeenCalledTimes(1)
    expect(store.view).toBe('compose')

    store.navigate({ name: 'compose' }) // leaving a dashboard starts fresh
    expect(stop).toHaveBeenCalled()
    expect(store.step).toBe('brief')
    expect(store.campaign).toBeNull()
    expect(store.standalone).toBe(false)
  })
})

describe('content per language', () => {
  const bilingual: SegmentResponse = {
    ...segment,
    languages: [
      { language: 'tr', label: 'Türkçe', reachable: 291 },
      { language: 'en', label: 'İngilizce', reachable: 32 },
    ],
  }
  const english: Content = { sms: 'Hi {{first_name}}, 30% off!', email: { subject: 'An offer', body: 'Hi' } }

  /** Makes streamLocalize replay the given events. */
  function scriptLocalize(...events: LocalizeEvent[]) {
    fake.streamLocalize.mockImplementation(
      async (_brief: string, _content: Content, _from: string, _to: string[], onEvent: (e: LocalizeEvent) => void) => {
        for (const e of events) onEvent(e)
      },
    )
  }

  const localized: LocalizeEvent = {
    event: 'result',
    data: { variants: { en: english }, lengths: { en: { sms: 27, email_subject: 8 } }, limits: result.limits, variables: result.variables, attempts: 1 },
  }

  async function bilingualAtContent() {
    const store = useCampaignStore()
    store.brief = 'brief'
    fake.generateSegment.mockResolvedValue(bilingual)
    await store.generateSegment()
    script({ event: 'attempt', data: { attempt: 1 } }, { event: 'result', data: result })
    await store.generateContent()
    return store
  }

  it('writes the main text in the language most of the audience reads', async () => {
    const store = await bilingualAtContent()
    expect(fake.streamContent.mock.calls[0]?.[1]).toBe('tr')
    expect(store.contentLanguage).toBe('tr')
    expect(store.missingLanguages).toEqual(['en'])
    expect(fake.streamLocalize).not.toHaveBeenCalled() // the other languages wait for the marketer

    // An audience that mostly reads English gets English first.
    fake.countSegment.mockResolvedValue({ ...bilingual, languages: [...bilingual.languages!].reverse() })
    await store.applyRule(bilingual.rule)
    await store.generateContent()
    expect(fake.streamContent.mock.calls[1]?.[1]).toBe('en')
    expect(store.missingLanguages).toEqual(['tr'])
  })

  it('asks for all the other languages in one request, from the text as edited', async () => {
    const store = await bilingualAtContent()
    store.content.sms = 'Elle düzenlendi {{first_name}}'
    scriptLocalize(
      { event: 'attempt', data: { attempt: 1 } },
      { event: 'delta', data: { language: 'en', field: 'sms', text: 'Hi {{first' } },
      { event: 'delta', data: { language: 'en', field: 'sms', text: '_name}}' } },
      localized,
    )

    await store.localize()

    expect(fake.streamLocalize).toHaveBeenCalledTimes(1)
    const [brief, source, from, to] = fake.streamLocalize.mock.calls[0] ?? []
    expect([brief, from, to]).toEqual(['brief', 'tr', ['en']])
    expect(source).toEqual({ sms: 'Elle düzenlendi {{first_name}}', email: result.content.email })
    expect(store.variants.en).toEqual(english) // the validated result replaced the streamed preview
    expect(store.missingLanguages).toEqual([])
    expect(store.localizing).toBe(false)
    expect(store.activeLanguage).toBe('en')
    expect(store.activeContent).toEqual(english)
    expect(store.contentValid).toBe(true)
  })

  it('shows the text while it streams and clears it when an attempt is rejected', async () => {
    const store = await bilingualAtContent()
    let duringFirst = ''
    fake.streamLocalize.mockImplementation(async (_b: string, _c: Content, _f: string, _t: string[], onEvent: (e: LocalizeEvent) => void) => {
      onEvent({ event: 'attempt', data: { attempt: 1 } })
      onEvent({ event: 'delta', data: { language: 'en', field: 'sms', text: 'Hi {{nickname}}' } })
      duringFirst = store.variants.en?.sms ?? ''
      onEvent({ event: 'rejected', data: { attempt: 1, errors: [{ path: 'en.sms', message: 'bilinmeyen bir değişken' }] } })
      onEvent({ event: 'attempt', data: { attempt: 2 } })
      expect(store.variants.en).toBeUndefined()
      onEvent(localized)
    })
    await store.localize()
    expect(duringFirst).toBe('Hi {{nickname}}')
    expect(store.localizeRejections).toEqual([{ attempt: 1, errors: [{ path: 'en.sms', message: 'bilinmeyen bir değişken' }] }])
    expect(store.variants.en).toEqual(english)
  })

  it('sends every language on approval, the main one as the default', async () => {
    const store = await bilingualAtContent()
    scriptLocalize(localized)
    await store.localize()
    store.variants.en!.sms = 'Hi {{first_name}}, edited'
    fake.createCampaign.mockResolvedValue({ id: 7, as_of: 'now', send_at: 'now', queued: { sms: 1, email: 1 } })
    fake.watchCampaign.mockReturnValue(() => {})

    await store.approve()

    expect(fake.createCampaign.mock.calls[0]?.[2]).toEqual({
      default: 'tr',
      variants: { tr: result.content, en: { ...english, sms: 'Hi {{first_name}}, edited' } },
    })
  })

  it('does not send while a language of the audience has no text', async () => {
    const store = await bilingualAtContent()
    fake.createCampaign.mockResolvedValue({ id: 7, as_of: 'now', send_at: 'now', queued: { sms: 1, email: 1 } })
    fake.watchCampaign.mockReturnValue(() => {})

    // Forgetting the other languages cannot send the campaign: nobody gets
    // a text in a language they do not read.
    expect(store.languagesResolved).toBe(false)
    await store.approve()
    expect(fake.createCampaign).not.toHaveBeenCalled()
    expect(store.step).toBe('content')

    scriptLocalize(localized)
    await store.localize()
    expect(store.languagesResolved).toBe(true)
    await store.approve()
    expect(fake.createCampaign).toHaveBeenCalledTimes(1)

    // Generating the main text again closes approval until they are written again.
    const again = await bilingualAtContent()
    scriptLocalize(localized)
    await again.localize()
    await again.generateContent()
    expect(again.languagesResolved).toBe(false)
  })

  it('a campaign for one language narrows its audience instead', async () => {
    const store = await bilingualAtContent()
    // The marketer goes back and adds "language: Turkish" to the rule.
    fake.countSegment.mockResolvedValue({ ...bilingual, languages: [bilingual.languages![0]!] })
    await store.applyRule(bilingual.rule)
    await store.generateContent()
    expect(store.missingLanguages).toEqual([])
    expect(store.languagesResolved).toBe(true)
  })

  it('blocks approval while any language is over a limit, not only the visible one', async () => {
    const store = await bilingualAtContent()
    scriptLocalize(localized)
    await store.localize()
    store.activeLanguage = 'tr'
    store.variants.en!.sms = 'x'.repeat(151) + '{{first_name}}' // 161 with the longest name
    expect(store.invalidLanguages).toEqual(['en'])
    expect(store.contentValid).toBe(false)
    expect(store.measured.sms.length).toBe(32) // the counter still shows the visible language

    store.activeLanguage = 'en'
    expect(store.measured.sms.length).toBe(161)
    store.variants.en!.sms = english.sms
    expect(store.contentValid).toBe(true)
  })

  it('notices when the main text changes after the others were written from it', async () => {
    const store = await bilingualAtContent()
    scriptLocalize(localized)
    await store.localize()
    expect(store.translationsStale).toBe(false)
    store.content.email.subject = 'Yeni konu'
    expect(store.translationsStale).toBe(true)
    await store.localize() // written again from the new text
    expect(store.translationsStale).toBe(false)
    expect(fake.streamLocalize.mock.calls[1]?.[1]).toMatchObject({ email: { subject: 'Yeni konu' } })
  })

  it('drops the other languages when the main text is generated again', async () => {
    const store = await bilingualAtContent()
    scriptLocalize(localized)
    await store.localize()
    await store.generateContent()
    expect(store.variants).toEqual({})
    expect(store.activeLanguage).toBe('tr')
    expect(store.missingLanguages).toEqual(['en'])
  })

  it('keeps the main text and shows the error when the other languages fail', async () => {
    const store = await bilingualAtContent()
    scriptLocalize(
      { event: 'attempt', data: { attempt: 1 } },
      { event: 'delta', data: { language: 'en', field: 'sms', text: 'Hi' } },
      { event: 'error', data: { code: 'llm_output_invalid', message: 'Model limitlere uyan bir içerik üretemedi', details: [{ path: 'en.sms', message: 'x' }] } },
    )
    await store.localize()
    expect(store.variants).toEqual({})
    expect(store.localizeError?.message).toBe('Model limitlere uyan bir içerik üretemedi')
    expect(store.content).toEqual(result.content)
    expect(store.activeLanguage).toBe('tr') // back to a language that has text
    expect(store.contentValid).toBe(true) // still sendable in the main language

    fake.streamLocalize.mockRejectedValue(new ApiError(0, 'network', 'Sunucuya ulaşılamadı'))
    await store.localize()
    expect(store.localizeError?.message).toBe('Sunucuya ulaşılamadı')
    expect(store.localizing).toBe(false)
  })

  it('does nothing when the audience reads a single language', async () => {
    const store = await storeAtContent()
    await store.localize()
    expect(fake.streamLocalize).not.toHaveBeenCalled()
    expect(store.missingLanguages).toEqual([])
  })
})
