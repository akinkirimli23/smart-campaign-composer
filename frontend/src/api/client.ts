import { readEventStream } from '../lib/sse'
import { browserTimeZone } from '../lib/time'
import type {
  CampaignCreated,
  CampaignStatus,
  CampaignSummary,
  Content,
  ContentEvent,
  FieldError,
  LocalizedContent,
  LocalizeEvent,
  Rule,
  RuleOptions,
  SegmentResponse,
} from './types'

/** An error response from the API: { error: { code, message, details } }. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly details: FieldError[] = [],
  ) {
    super(message)
  }
}

async function toApiError(response: Response): Promise<ApiError> {
  try {
    const body = (await response.json()) as { error?: { code?: string; message?: string; details?: FieldError[] } }
    if (body.error) {
      return new ApiError(response.status, body.error.code ?? 'unknown', body.error.message ?? 'Bilinmeyen hata', body.error.details ?? [])
    }
  } catch {
    // not JSON; fall through
  }
  // No JSON body: the answer came from the proxy, not from the API. 502-504
  // there mean the API is down or still starting.
  if (response.status >= 502 && response.status <= 504) {
    return new ApiError(response.status, 'unavailable', 'Sunucuya şu an ulaşılamıyor. Birkaç saniye sonra yeniden dene.')
  }
  return new ApiError(response.status, 'unknown', `Sunucu ${response.status} yanıtı verdi`)
}

async function postJSON<T>(path: string, body: unknown): Promise<T> {
  let response: Response
  try {
    response = await fetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, 'network', 'Sunucuya ulaşılamadı')
  }
  if (!response.ok) throw await toApiError(response)
  return (await response.json()) as T
}

async function getJSON<T>(path: string): Promise<T> {
  let response: Response
  try {
    response = await fetch(path)
  } catch {
    throw new ApiError(0, 'network', 'Sunucuya ulaşılamadı')
  }
  if (!response.ok) throw await toApiError(response)
  return (await response.json()) as T
}

/**
 * POSTs and reads the answer as a server-sent event stream (EventSource
 * cannot POST). Resolves when the stream ends; request-level problems reject
 * with an ApiError before any event.
 */
async function postStream<E>(path: string, body: unknown, onEvent: (event: E) => void, signal?: AbortSignal): Promise<void> {
  let response: Response
  try {
    response = await fetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' },
      body: JSON.stringify(body),
      signal,
    })
  } catch (err) {
    if (signal?.aborted) throw err
    throw new ApiError(0, 'network', 'Sunucuya ulaşılamadı')
  }
  if (!response.ok || !response.body) throw await toApiError(response)
  await readEventStream(response.body, (message) => {
    onEvent({ event: message.event, data: JSON.parse(message.data) } as E)
  })
}

export const api = {
  /** Allowed values and bounds for the rule editor. */
  segmentOptions: () => getJSON<RuleOptions>('/api/segments/options'),

  // The time zone lets the backend read "yarın saat 10" the way the marketer means it.
  generateSegment: (brief: string) =>
    postJSON<SegmentResponse>('/api/segments/generate', { brief, time_zone: browserTimeZone() }),

  /** Validates an edited rule and counts its audience. */
  countSegment: (rule: unknown) => postJSON<SegmentResponse>('/api/segments/count', { rule }),

  /** The approval: the backend validates rule and content again before queueing. */
  createCampaign: (brief: string, rule: Rule, content: LocalizedContent, sendAt?: string) =>
    postJSON<CampaignCreated>('/api/campaigns', { brief, rule, content, ...(sendAt ? { send_at: sendAt } : {}) }),

  /** Recent campaigns, newest first. */
  listCampaigns: async () => (await getJSON<{ campaigns: CampaignSummary[] }>('/api/campaigns')).campaigns,

  /** Stops whatever has not been sent yet; returns the campaign's status. */
  cancelCampaign: (id: number) => postJSON<CampaignStatus>(`/api/campaigns/${id}/cancel`, {}),

  /**
   * Streams content generation in one language (the one most of the audience
   * reads). The final event is "result" or "error".
   */
  streamContent: (brief: string, language: string, onEvent: (event: ContentEvent) => void, signal?: AbortSignal) =>
    postStream<ContentEvent>('/api/content/generate', { brief, language }, onEvent, signal),

  /**
   * Streams the texts for the other languages, written from the main text.
   * One request, and one model call behind it, however many languages.
   */
  streamLocalize: (
    brief: string,
    content: Content,
    from: string,
    to: string[],
    onEvent: (event: LocalizeEvent) => void,
    signal?: AbortSignal,
  ) => postStream<LocalizeEvent>('/api/content/localize', { brief, content, from, to }, onEvent, signal),

  /**
   * Follows a campaign's live status. EventSource reconnects by itself if the
   * connection drops; the stream is closed here once the server says "done".
   * Returns a function that stops watching.
   */
  watchCampaign(
    id: number,
    handlers: { onStatus: (status: CampaignStatus) => void; onDone: () => void; onError: (message: string) => void },
  ): () => void {
    const source = new EventSource(`/api/campaigns/${id}/events`)
    source.addEventListener('status', (e) => handlers.onStatus(JSON.parse((e as MessageEvent<string>).data) as CampaignStatus))
    source.addEventListener('done', () => {
      source.close()
      handlers.onDone()
    })
    source.addEventListener('error', (e) => {
      // A server-sent "error" event carries data; a connection error does not
      // and EventSource retries on its own.
      const data = (e as MessageEvent<string>).data
      if (typeof data === 'string') {
        source.close()
        handlers.onError((JSON.parse(data) as { message: string }).message)
      } else if (source.readyState === EventSource.CLOSED) {
        // The browser gave up for good, which it does when the answer was not
        // a stream at all: most likely there is no such campaign.
        handlers.onError('Kampanya bulunamadı ya da sunucuya ulaşılamıyor.')
      }
    })
    return () => source.close()
  },
}

export type Api = typeof api
