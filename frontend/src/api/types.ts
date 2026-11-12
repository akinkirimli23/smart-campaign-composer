// Shapes of the backend's JSON. Field names follow the API (snake_case).

export interface FieldError {
  path: string
  message: string
}

export type SetField = 'city' | 'language' | 'gender'

/** City, language or gender is (not) one of a closed set of values. */
export interface SetCondition {
  type: 'attribute'
  field: SetField
  op: 'in' | 'not_in'
  values: string[]
}

export interface AgeCondition {
  type: 'attribute'
  field: 'age'
  op: 'between'
  min: number
  max: number
}

/** Did (not) add to cart / purchase within the last N days. */
export interface EventCondition {
  type: 'event'
  event: string
  category: string | null
  within_days: number
  exists: boolean
  /** At least this many times; null means at least once. */
  min_count: number | null
}

export interface SignupCondition {
  type: 'signup'
  op: 'within' | 'older_than'
  days: number
}

export interface MetricCondition {
  type: 'metric'
  metric: string
  op: 'gte' | 'lte'
  value: number
  /** Only purchases in the last N days; null means all time. */
  within_days: number | null
}

export type Condition = SetCondition | AgeCondition | EventCondition | SignupCondition | MetricCondition

export interface Rule {
  match: 'all' | 'any'
  conditions: Condition[]
}

export interface Option {
  value: string
  label: string
}

/** Allowed values and bounds of a rule, from GET /api/segments/options. */
export interface RuleOptions {
  cities: Option[]
  languages: Option[]
  genders: Option[]
  categories: Option[]
  events: Option[]
  metrics: Option[]
  limits: {
    max_conditions: number
    min_within_days: number
    max_within_days: number
    min_age: number
    max_age: number
    max_min_count: number
    max_signup_days: number
    max_spent: number
    max_orders: number
  }
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
  /** Not sent because the campaign was cancelled first. */
  cancelled: number
}

export interface CampaignStatus {
  id: number
  created_at: string
  /** When delivery starts (or started), RFC 3339. */
  send_at: string
  cancelled_at?: string
  channels: Record<Channel, ChannelCounts>
  done: boolean
}

export interface CampaignCreated {
  id: number
  as_of: string
  send_at: string
  queued: Record<Channel, number>
}
