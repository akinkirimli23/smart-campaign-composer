// Helpers for the rule editor: what kinds of condition exist, what a new one
// looks like, and how to keep a draft tidy. Nothing here decides whether a
// rule is valid; the backend does, when the draft is applied.

import type { Condition, Rule, RuleOptions, SetField } from '../api/types'

/** The kinds of condition a marketer can add, in the order they are offered. */
export type ConditionKind = SetField | 'age' | 'event' | 'signup' | 'metric'

export const KIND_LABELS: Record<ConditionKind, string> = {
  city: 'Şehir',
  language: 'Dil',
  gender: 'Cinsiyet',
  age: 'Yaş',
  event: 'Davranış (sepet, satın alma)',
  signup: 'Üyelik tarihi',
  metric: 'Harcama / sipariş sayısı',
}

export const KINDS = Object.keys(KIND_LABELS) as ConditionKind[]

export function kindOf(c: Condition): ConditionKind {
  return c.type === 'attribute' ? c.field : c.type
}

/**
 * Kinds that can still be added. City, language, gender and age may appear
 * once each (the backend rejects a second one); the others may repeat.
 */
export function addableKinds(rule: Rule, options: RuleOptions): ConditionKind[] {
  if (rule.conditions.length >= options.limits.max_conditions) return []
  const used = new Set(rule.conditions.map(kindOf))
  return KINDS.filter((kind) => !(['city', 'language', 'gender', 'age'].includes(kind) && used.has(kind)))
}

/** A new condition with sensible starting values. */
export function defaultCondition(kind: ConditionKind, options: RuleOptions): Condition {
  switch (kind) {
    case 'city':
      return { type: 'attribute', field: 'city', op: 'in', values: [options.cities[0]?.value ?? ''] }
    case 'language':
      return { type: 'attribute', field: 'language', op: 'in', values: [options.languages[0]?.value ?? ''] }
    case 'gender':
      return { type: 'attribute', field: 'gender', op: 'in', values: [options.genders[0]?.value ?? ''] }
    case 'age':
      return { type: 'attribute', field: 'age', op: 'between', min: 25, max: 34 }
    case 'event':
      return { type: 'event', event: options.events[0]?.value ?? '', category: null, within_days: 30, exists: true, min_count: null }
    case 'signup':
      return { type: 'signup', op: 'within', days: 30 }
    case 'metric':
      return { type: 'metric', metric: options.metrics[0]?.value ?? '', op: 'gte', value: 1000, within_days: null }
  }
}

/** The values a closed-set field can take. */
export function valuesOf(field: SetField, options: RuleOptions) {
  return field === 'city' ? options.cities : field === 'language' ? options.languages : options.genders
}

/** A deep copy, so editing a draft never touches the rule in the store. */
export function cloneRule(rule: Rule): Rule {
  return JSON.parse(JSON.stringify(rule)) as Rule
}

/**
 * Prepares a draft for the backend: number inputs can hold "" or NaN while
 * the user types, and "at least 1 time" is the same as no count. Unusable
 * numbers are sent as 0 so the backend reports them against the right field.
 */
export function normaliseRule(rule: Rule): Rule {
  const int = (v: unknown): number => {
    const n = typeof v === 'number' ? v : Number.parseInt(String(v), 10)
    return Number.isFinite(n) ? Math.trunc(n) : 0
  }
  const optionalInt = (v: unknown): number | null => (v === null || v === undefined || v === '' ? null : int(v))

  return {
    match: rule.match,
    conditions: rule.conditions.map((c): Condition => {
      switch (c.type) {
        case 'event': {
          const count = c.exists ? optionalInt(c.min_count) : null
          return { ...c, within_days: int(c.within_days), category: c.category || null, min_count: count !== null && count <= 1 ? null : count }
        }
        case 'signup':
          return { ...c, days: int(c.days) }
        case 'metric':
          return { ...c, value: int(c.value), within_days: optionalInt(c.within_days) }
        case 'attribute':
          return c.field === 'age' ? { ...c, min: int(c.min), max: int(c.max) } : { ...c, values: [...c.values] }
      }
    }),
  }
}

/** Whether two rules are the same once normalised (used for "unsaved changes"). */
export function sameRule(a: Rule, b: Rule): boolean {
  return JSON.stringify(normaliseRule(a)) === JSON.stringify(normaliseRule(b))
}

/** Splits backend errors into those of condition i and the rest. */
export function errorsFor(details: { path: string; message: string }[], index: number): string[] {
  const prefix = `conditions[${index}]`
  return details
    .filter((d) => d.path === prefix || d.path.startsWith(prefix + '.') || d.path.startsWith(prefix + '['))
    .map((d) => d.message)
}

export function generalErrors(details: { path: string; message: string }[]): { path: string; message: string }[] {
  return details.filter((d) => !/^conditions\[\d+\]/.test(d.path))
}
