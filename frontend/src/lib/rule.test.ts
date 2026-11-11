import { describe, expect, it } from 'vitest'
import type { Rule, RuleOptions } from '../api/types'
import { addableKinds, cloneRule, defaultCondition, errorsFor, generalErrors, KINDS, kindOf, normaliseRule, sameRule } from './rule'

const options: RuleOptions = {
  cities: [{ value: 'İstanbul', label: 'İstanbul' }, { value: 'Ankara', label: 'Ankara' }],
  languages: [{ value: 'tr', label: 'Türkçe' }, { value: 'en', label: 'İngilizce' }],
  genders: [{ value: 'female', label: 'Kadın' }, { value: 'male', label: 'Erkek' }],
  categories: [{ value: 'electronics', label: 'Elektronik' }],
  events: [{ value: 'cart_add', label: 'Sepete ürün ekleme' }, { value: 'purchase', label: 'Satın alma' }],
  metrics: [{ value: 'total_spent', label: 'Toplam harcama (TL)' }, { value: 'order_count', label: 'Sipariş sayısı' }],
  limits: {
    max_conditions: 10, min_within_days: 1, max_within_days: 365, min_age: 0, max_age: 120,
    max_min_count: 100, max_signup_days: 3650, max_spent: 1000000, max_orders: 1000,
  },
}

const example: Rule = {
  match: 'all',
  conditions: [
    { type: 'attribute', field: 'city', op: 'in', values: ['İstanbul'] },
    { type: 'event', event: 'cart_add', category: null, within_days: 30, exists: true, min_count: null },
    { type: 'event', event: 'purchase', category: null, within_days: 30, exists: false, min_count: null },
  ],
}

describe('defaultCondition', () => {
  it('builds a condition of the requested kind for every kind', () => {
    for (const kind of KINDS) expect(kindOf(defaultCondition(kind, options))).toBe(kind)
  })

  it('starts from values the backend accepts', () => {
    expect(defaultCondition('city', options)).toEqual({ type: 'attribute', field: 'city', op: 'in', values: ['İstanbul'] })
    expect(defaultCondition('event', options)).toEqual({ type: 'event', event: 'cart_add', category: null, within_days: 30, exists: true, min_count: null })
    expect(defaultCondition('metric', options)).toEqual({ type: 'metric', metric: 'total_spent', op: 'gte', value: 1000, within_days: null })
  })
})

describe('addableKinds', () => {
  it('offers each attribute once but lets behaviours and metrics repeat', () => {
    const kinds = addableKinds(example, options)
    expect(kinds).not.toContain('city')
    expect(kinds).toEqual(['language', 'gender', 'age', 'event', 'signup', 'metric'])
  })

  it('offers nothing once the rule is full', () => {
    const full: Rule = { match: 'all', conditions: Array.from({ length: 10 }, () => defaultCondition('event', options)) }
    expect(addableKinds(full, options)).toEqual([])
  })
})

describe('normaliseRule', () => {
  it('leaves a rule from the backend unchanged', () => {
    expect(normaliseRule(example)).toEqual(example)
  })

  it('turns what number inputs hold while typing into numbers the backend can judge', () => {
    const draft = cloneRule(example)
    const event = draft.conditions[1]
    if (event?.type !== 'event') throw new Error('test setup')
    event.within_days = '' as unknown as number // emptied input
    event.min_count = '3' as unknown as number
    expect(normaliseRule(draft).conditions[1]).toMatchObject({ within_days: 0, min_count: 3 })
  })

  it('treats "at least 1 time" as no count, and drops the count when the event did not happen', () => {
    const rule: Rule = {
      match: 'all',
      conditions: [
        { type: 'event', event: 'purchase', category: '' as unknown as null, within_days: 7, exists: true, min_count: 1 },
        { type: 'event', event: 'cart_add', category: null, within_days: 7, exists: false, min_count: 5 },
      ],
    }
    expect(normaliseRule(rule).conditions).toEqual([
      { type: 'event', event: 'purchase', category: null, within_days: 7, exists: true, min_count: null },
      { type: 'event', event: 'cart_add', category: null, within_days: 7, exists: false, min_count: null },
    ])
  })

  it('keeps "all time" as null for metrics and truncates decimals', () => {
    const rule: Rule = { match: 'any', conditions: [{ type: 'metric', metric: 'order_count', op: 'lte', value: 2.9, within_days: null }] }
    expect(normaliseRule(rule).conditions[0]).toEqual({ type: 'metric', metric: 'order_count', op: 'lte', value: 2, within_days: null })
  })
})

describe('draft handling', () => {
  it('cloneRule copies deeply, so a draft never changes the original', () => {
    const draft = cloneRule(example)
    const city = draft.conditions[0]
    if (city?.type !== 'attribute' || city.field === 'age') throw new Error('test setup')
    city.values.push('Ankara')
    draft.conditions.pop()
    expect(example.conditions).toHaveLength(3)
    expect(example.conditions[0]).toMatchObject({ values: ['İstanbul'] })
    expect(sameRule(draft, example)).toBe(false)
    expect(sameRule(cloneRule(example), example)).toBe(true)
  })
})

describe('error placement', () => {
  const details = [
    { path: 'conditions[1].within_days', message: '400 geçersiz' },
    { path: 'conditions[1]', message: 'çelişiyor' },
    { path: 'conditions[10].values[0]', message: 'onuncu' },
    { path: 'conditions', message: 'en az bir koşul gerekli' },
    { path: 'match', message: 'geçersiz' },
  ]

  it('puts each error under its own condition', () => {
    expect(errorsFor(details, 1)).toEqual(['400 geçersiz', 'çelişiyor'])
    expect(errorsFor(details, 10)).toEqual(['onuncu'])
    expect(errorsFor(details, 0)).toEqual([])
  })

  it('keeps rule-level errors separate', () => {
    expect(generalErrors(details).map((d) => d.path)).toEqual(['conditions', 'match'])
  })
})
