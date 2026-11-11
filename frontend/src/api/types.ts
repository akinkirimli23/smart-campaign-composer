// Shapes of the backend's JSON. Field names follow the API (snake_case).

export interface FieldError {
  path: string
  message: string
}

export interface Rule {
  match: 'all' | 'any'
  conditions: Record<string, unknown>[]
}

export interface Audience {
  total: number
  sms: number
  email: number
  unreachable: number
}

export interface Assumption {
  condition_index: number
  phrase: string
  interpretation: string
}

export interface SegmentResponse {
  status: 'ok' | 'unsupported'
  rule?: Rule
  description?: { summary: string; conditions: string[] }
  assumptions?: Assumption[]
  audience?: Audience
  as_of?: string
  attempts?: number
  unsupported?: { reason: string; phrases: string[] }
}

export interface Content {
  sms: string
  email: { subject: string; body: string }
}

/** Worst-case lengths of the limited fields, as measured by the backend. */
export interface Lengths {
  sms: number
  email_subject: number
}

/** Payload of the content stream's final "result" event. */
export interface ContentResult {
  content: Content
  lengths: Lengths
  limits: Lengths
  /** Longest value, in characters, each template variable can take. */
  variables: Record<string, number>
  attempts: number
}

export type ContentField = 'sms' | 'email.subject' | 'email.body'

export type ContentEvent =
  | { event: 'attempt'; data: { attempt: number } }
  | { event: 'delta'; data: { field: ContentField; text: string } }
  | { event: 'rejected'; data: { attempt: number; errors: FieldError[] } }
  | { event: 'result'; data: ContentResult }
  | { event: 'error'; data: { code: string; message: string; details?: FieldError[] } }

export type Channel = 'sms' | 'email'

export interface ChannelCounts {
  total: number
  pending: number
  sending: number
  sent: number
  failed: number
  skipped: number
}

export interface CampaignStatus {
  id: number
  created_at: string
  channels: Record<Channel, ChannelCounts>
  done: boolean
}

export interface CampaignCreated {
  id: number
  as_of: string
  queued: Record<Channel, number>
}
