import { readEventStream } from '../lib/sse'
import type {
  CampaignCreated,
  CampaignStatus,
  Content,
  ContentEvent,
  FieldError,
  Rule,
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

export const api = {
  generateSegment: (brief: string) => postJSON<SegmentResponse>('/api/segments/generate', { brief }),

  /** Validates an edited rule and counts its audience. */
  countSegment: (rule: unknown) => postJSON<SegmentResponse>('/api/segments/count', { rule }),

  /** The approval: the backend validates rule and content again before queueing. */
  createCampaign: (brief: string, rule: Rule, content: Content) =>
    postJSON<CampaignCreated>('/api/campaigns', { brief, rule, content }),

  /**
   * Streams content generation. Resolves when the stream ends; the final
   * event is "result" or "error". Request-level problems (empty brief, ...)
   * reject with an ApiError before any event.
   */
  async streamContent(brief: string, onEvent: (event: ContentEvent) => void, signal?: AbortSignal): Promise<void> {
    let response: Response
    try {
      response = await fetch('/api/content/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' },
        body: JSON.stringify({ brief }),
        signal,
      })
    } catch (err) {
      if (signal?.aborted) throw err
      throw new ApiError(0, 'network', 'Sunucuya ulaşılamadı')
    }
    if (!response.ok || !response.body) throw await toApiError(response)
    await readEventStream(response.body, (message) => {
      onEvent({ event: message.event, data: JSON.parse(message.data) } as ContentEvent)
    })
  },

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
      }
    })
    return () => source.close()
  },
}

export type Api = typeof api
