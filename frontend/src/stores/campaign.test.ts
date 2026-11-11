import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import type { CampaignStatus, ContentEvent, ContentResult, SegmentResponse } from '../api/types'

// The store is tested against a fake API: no network, no backend.
const fake = vi.hoisted(() => ({
  generateSegment: vi.fn(),
  countSegment: vi.fn(),
  segmentOptions: vi.fn(),
  createCampaign: vi.fn(),
  streamContent: vi.fn(),
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
  fake.streamContent.mockImplementation(async (_brief: string, onEvent: (e: ContentEvent) => void) => {
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
    fake.streamContent.mockImplementation(async (_b: string, onEvent: (e: ContentEvent) => void) => {
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
    fake.streamContent.mockImplementation(async (_b: string, onEvent: (e: ContentEvent) => void) => {
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
    fake.createCampaign.mockResolvedValue({ id: 7, as_of: 'now', queued: { sms: 222, email: 254 } })
    let handlers: { onStatus: (s: CampaignStatus) => void; onDone: () => void } | undefined
    fake.watchCampaign.mockImplementation((_id: number, h: typeof handlers) => {
      handlers = h
      return () => {}
    })

    await store.approve()

    expect(fake.createCampaign).toHaveBeenCalledWith('brief', segment.rule, {
      sms: 'Elle düzenlendi {{first_name}}',
      email: result.content.email,
    })
    expect(store.step).toBe('dashboard')
    expect(fake.watchCampaign).toHaveBeenCalledWith(7, expect.anything())

    const status: CampaignStatus = {
      id: 7,
      created_at: 'now',
      done: false,
      channels: {
        sms: { total: 222, pending: 100, sending: 22, sent: 100, failed: 0, skipped: 0 },
        email: { total: 254, pending: 254, sending: 0, sent: 0, failed: 0, skipped: 0 },
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
    fake.createCampaign.mockResolvedValue({ id: 7, as_of: 'now', queued: { sms: 1, email: 1 } })
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
