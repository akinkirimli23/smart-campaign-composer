// Helpers for the rule editor: what kinds of condition exist, what a new one
// looks like, and how to keep a draft tidy. Nothing here decides whether a
// rule is valid; the backend does, when the draft is applied.

import type { Condition, GroupCondition, PlainCondition, Rule, RuleOptions, SetField } from '../api/types'

/** The kinds of condition a marketer can add, in the order they are offered. */
export type ConditionKind = SetField | 'age' | 'event' | 'signup' | 'metric'

export const KIND_LABELS: Record<ConditionKind, string> = {
  city: 'Şehir',
  language: 'Dil',
  gender: 'Cinsiyet',
  age: 'Yaş',
  event: 'Davranış (sepet, satın alma)',
  signup: 'Üyelik tarihi',
  metric: 'Harcama / sipariş (toplam, ortalama, adet)',
}

export const KINDS = Object.keys(KIND_LABELS) as ConditionKind[]

export function kindOf(c: PlainCondition): ConditionKind {
  return c.type === 'attribute' ? c.field : c.type
}

// --- groups ---------------------------------------------------------------------
// A group joins its members with the connector the rule itself does not use:
// in a rule of "all of these", a group is "at least one of these". The editor
// therefore never asks for a group's connector; it follows the rule's.

export function opposite(match: Rule['match']): Rule['match'] {
  return match === 'all' ? 'any' : 'all'
}

export const GROUP_LABELS: Record<Rule['match'], string> = {
  any: 'Şunlardan en az biri',
  all: 'Şunların hepsi',
}

export function isGroup(c: Condition): c is GroupCondition {
  return c.type === 'group'
}

/** How many plain conditions a rule holds, counting those inside groups. */
export function leafCount(rule: Rule): number {
  return rule.conditions.reduce((n, c) => n + (isGroup(c) ? c.conditions.length : 1), 0)
}

/**
 * Kinds that can still be added to one list: the rule's own conditions, or
 * the members of one group. City, language, gender and age may appear once
 * per list (the backend rejects a second one); the others may repeat. The
 * limit counts every plain condition of the rule.
 */
export function addableKinds(scope: Condition[], rule: Rule, options: RuleOptions): ConditionKind[] {
  if (leafCount(rule) >= options.limits.max_conditions) return []
  const used = new Set(scope.filter((c): c is PlainCondition => !isGroup(c)).map(kindOf))
  return KINDS.filter((kind) => !(['city', 'language', 'gender', 'age'].includes(kind) && used.has(kind)))
}

/** A group needs two members, so there must be room for two more conditions. */
export function canAddGroup(rule: Rule, options: RuleOptions): boolean {
  return leafCount(rule) + 2 <= options.limits.max_conditions
}

/** A new group with two members to start from. */
export function defaultGroup(rule: Rule, options: RuleOptions): GroupCondition {
  return {
    type: 'group',
    match: opposite(rule.match),
    conditions: [defaultCondition('event', options), defaultCondition('metric', options)],
  }
}

/** A new condition with sensible starting values. */
export function defaultCondition(kind: ConditionKind, options: RuleOptions): PlainCondition {
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
      return { type: 'event', event: options.events[0]?.value ?? '', category: null, within_days: 30, exists: true, min_count: null, max_count: null }
    case 'signup':
      return { type: 'signup', op: 'within', days: 30 }
    case 'metric':
      return { type: 'metric', metric: options.metrics[0]?.value ?? '', op: 'gte', value: 1000, within_days: null, category: null }
  }
}

/** The values a closed-set field can take. */
export function valuesOf(field: SetField, options: RuleOptions) {
  return field === 'city' ? options.cities : field === 'language' ? options.languages : options.genders
}

/** Lower case without Turkish diacritics, so "sanli" finds Şanlıurfa. */
export function foldTr(text: string): string {
  return text
    .toLocaleLowerCase('tr')
    .replace(/ı/g, 'i')
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
}

/**
 * The options still to pick for a search: those not chosen yet whose label
 * matches, names that start with the query before names that contain it.
 * The order of the options is otherwise kept.
 */
export function searchOptions<T extends { value: string; label: string }>(options: T[], query: string, chosen: string[]): T[] {
  const q = foldTr(query.trim())
  const open = options.filter((o) => !chosen.includes(o.value))
  if (!q) return open
  const starts = open.filter((o) => foldTr(o.label).startsWith(q))
  const contains = open.filter((o) => !starts.includes(o) && foldTr(o.label).includes(q))
  return [...starts, ...contains]
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

  const plain = (c: PlainCondition): PlainCondition => {
    switch (c.type) {
      case 'event': {
        const count = c.exists ? optionalInt(c.min_count) : null
        return {
          ...c,
          within_days: int(c.within_days),
          category: c.category || null,
          min_count: count !== null && count <= 1 ? null : count,
          // "Did not do it" takes no counts at all.
          max_count: c.exists ? optionalInt(c.max_count) : null,
        }
      }
      case 'signup':
        return { ...c, days: int(c.days) }
      case 'metric':
        return { ...c, value: int(c.value), within_days: optionalInt(c.within_days), category: c.category || null }
      case 'attribute':
        return c.field === 'age' ? { ...c, min: int(c.min), max: int(c.max) } : { ...c, values: [...c.values] }
    }
  }

  return {
    match: rule.match,
    conditions: rule.conditions.map((c): Condition =>
      // A group always uses the connector the rule does not, also after the
      // rule's own connector was switched in the editor.
      isGroup(c) ? { type: 'group', match: opposite(rule.match), conditions: c.conditions.map(plain) } : plain(c),
    ),
  }
}

/** Whether two rules are the same once normalised (used for "unsaved changes"). */
export function sameRule(a: Rule, b: Rule): boolean {
  return JSON.stringify(normaliseRule(a)) === JSON.stringify(normaliseRule(b))
}

type Detail = { path: string; message: string }

function under(d: Detail, prefix: string): boolean {
  return d.path === prefix || d.path.startsWith(prefix + '.') || d.path.startsWith(prefix + '[')
}

/** Backend errors of the rule's condition i (for a group: all of them, members included). */
export function errorsFor(details: Detail[], index: number): string[] {
  return details.filter((d) => under(d, `conditions[${index}]`)).map((d) => d.message)
}

/** Backend errors of member j of the group at i. */
export function memberErrors(details: Detail[], index: number, member: number): string[] {
  return details.filter((d) => under(d, `conditions[${index}].conditions[${member}]`)).map((d) => d.message)
}

/** Backend errors about the group at i itself, not about one of its members. */
export function groupErrors(details: Detail[], index: number): string[] {
  const prefix = `conditions[${index}]`
  return details.filter((d) => under(d, prefix) && !d.path.startsWith(prefix + '.conditions[')).map((d) => d.message)
}

export function generalErrors(details: { path: string; message: string }[]): { path: string; message: string }[] {
  return details.filter((d) => !/^conditions\[\d+\]/.test(d.path))
}
