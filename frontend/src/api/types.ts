// Shapes of the backend's JSON. Field names follow the API (snake_case).

export interface FieldError {
  path: string
  message: string
}

export type SetField = 'city' | 'language' | 'gender' | 'loyalty_tier'

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
  /** At most this many times (and still at least once); null means no upper bound. */
  max_count: number | null
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
  /** Only purchases in this category; null means all categories. */
  category: string | null
}

export type PlainCondition = SetCondition | AgeCondition | EventCondition | SignupCondition | MetricCondition

/**
 * Plain conditions joined by their own AND / OR, inside a rule that uses the
 * other one: "(A or B) and C". One level only; a group never holds a group.
 */
export interface GroupCondition {
  type: 'group'
  match: 'all' | 'any'
  conditions: PlainCondition[]
}

export type Condition = PlainCondition | GroupCondition

export interface Rule {
  match: 'all' | 'any'
  conditions: Condition[]
  /** Every user, no conditions. Absent on every other rule. */
  everyone?: boolean
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
  loyalty_tiers: Option[]
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
  /** Messages left out only because the address was found invalid earlier. */
  suppressed?: number
  /** Messages left out because the person unsubscribed from that channel. */
  unsubscribed?: number
}

export interface Assumption {
  condition_index: number
  phrase: string
  interpretation: string
}

/** How many reachable people in the audience read one language. */
export interface AudienceLanguage {
  language: string
  /** The language's name, for display. */
  label: string
  reachable: number
}

export interface SegmentResponse {
  status: 'ok' | 'unsupported'
  rule?: Rule
  description?: {
    summary: string
    /** One line per condition; a group is one line naming all its members. */
    conditions: string[]
    /** Parallel to conditions: a group in parts, null for a plain condition. Absent for a flat rule. */
    groups?: ({ label: string; conditions: string[] } | null)[] | null
  }
  assumptions?: Assumption[]
  audience?: Audience
  /** Languages of the reachable audience, largest first. */
  languages?: AudienceLanguage[]
  /** Loyalty tiers of the reachable audience, lowest first; every tier is listed. */
  loyalty?: { tier: string; label: string; reachable: number }[]
  as_of?: string
  attempts?: number
  unsupported?: { reason: string; phrases: string[] }
  /** A send time read from the brief: a suggestion to pre-fill, not a decision. */
  schedule?: { send_at: string; phrase: string; interpretation: string }
}

export interface Content {
  sms: string
  email: { subject: string; body: string }
}

/**
 * What a campaign sends: one set of texts per language. A recipient gets the
 * set of their own language, or the default one when there is none for them.
 */
export interface LocalizedContent {
  default: string
  variants: Record<string, Content>
}

/** Worst-case lengths of the limited fields, as measured by the backend. */
export interface Lengths {
  sms: number
  email_subject: number
}

/** Payload of the content stream's final "result" event. */
export interface ContentResult {
  /** The language the texts were written in, when one was asked for. */
  language?: string
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

/** Payload of the localisation stream's final "result" event. */
export interface LocalizeResult {
  variants: Record<string, Content>
  lengths: Record<string, Lengths>
  limits: Lengths
  variables: Record<string, number>
  attempts: number
}

/** Like ContentEvent, for several languages at once: a delta names its language. */
export type LocalizeEvent =
  | { event: 'attempt'; data: { attempt: number } }
  | { event: 'delta'; data: { language: string; field: ContentField; text: string } }
  | { event: 'rejected'; data: { attempt: number; errors: FieldError[] } }
  | { event: 'result'; data: LocalizeResult }
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

export type MessageStatus = keyof Omit<ChannelCounts, 'total'>

/** One message of a campaign: who it is for and what became of it. */
export interface Recipient {
  user_id: number
  name: string
  channel: Channel
  /** Masked phone number or email address. */
  address: string
  language: string
  city: string
  status: MessageStatus
  attempts: number
  sent_at?: string
  /** The last failure reported for this message, if any. */
  error?: string
  /** Why this person is not sent to on this channel any more, if they are not. */
  suppression?: Suppression
  /** What was delivered to this person, personalised; absent until sent. */
  text?: string
  /** The email subject that went with it. */
  subject?: string
}

export type Suppression = 'unsubscribed' | 'invalid_address'


export interface RecipientPage {
  recipients: Recipient[]
  /** How many messages match the filter, not how many are on this page. */
  total: number
  offset: number
  /** Messages per status under the other filters: what each status button would list. */
  by_status: Partial<Record<MessageStatus, number>>
}

/** What narrows a recipient list; an empty value does not filter. */
export interface RecipientFilter {
  status: MessageStatus | ''
  channel: Channel | ''
  language: string
  city: string
  /** Part of the recipient's name. */
  q: string
  /** Only recipients who are suppressed for this reason. */
  suppression: Suppression | ''
}

export type CampaignState = 'scheduled' | 'sending' | 'done' | 'cancelled'

/** One row of the campaign list; counts are summed over both channels. */
export interface CampaignSummary {
  id: number
  brief: string
  created_at: string
  send_at: string
  cancelled_at?: string
  state: CampaignState
  messages: ChannelCounts
}

export interface CampaignStatus {
  id: number
  brief: string
  created_at: string
  /** When delivery starts (or started), RFC 3339. */
  send_at: string
  cancelled_at?: string
  /** Languages the campaign has texts in, the default first. */
  languages?: string[]
  channels: Record<Channel, ChannelCounts>
  done: boolean
}

export interface CampaignCreated {
  id: number
  as_of: string
  send_at: string
  queued: Record<Channel, number>
}
