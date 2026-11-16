import { describe, expect, it } from 'vitest'
import type { Rule, RuleOptions } from '../api/types'
import {
  addableKinds, canAddGroup, cloneRule, defaultCondition, defaultGroup, errorsFor, generalErrors, groupErrors, KINDS, kindOf,
  leafCount, memberErrors, normaliseRule, sameRule, searchOptions,
} from './rule'

const options: RuleOptions = {
  cities: [{ value: 'İstanbul', label: 'İstanbul' }, { value: 'Ankara', label: 'Ankara' }],
  languages: [{ value: 'tr', label: 'Türkçe' }, { value: 'en', label: 'İngilizce' }],
  genders: [{ value: 'female', label: 'Kadın' }, { value: 'male', label: 'Erkek' }],
  loyalty_tiers: [{ value: 'bronze', label: 'Bronz' }, { value: 'silver', label: 'Gümüş' }, { value: 'gold', label: 'Altın' }],
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
    { type: 'event', event: 'cart_add', category: null, within_days: 30, exists: true, min_count: null, max_count: null },
    { type: 'event', event: 'purchase', category: null, within_days: 30, exists: false, min_count: null, max_count: null },
  ],
}

describe('defaultCondition', () => {
  it('builds a condition of the requested kind for every kind', () => {
    for (const kind of KINDS) expect(kindOf(defaultCondition(kind, options))).toBe(kind)
  })

  it('starts from values the backend accepts', () => {
    expect(defaultCondition('city', options)).toEqual({ type: 'attribute', field: 'city', op: 'in', values: ['İstanbul'] })
    expect(defaultCondition('event', options)).toEqual({ type: 'event', event: 'cart_add', category: null, within_days: 30, exists: true, min_count: null, max_count: null })
    expect(defaultCondition('metric', options)).toEqual({ type: 'metric', metric: 'total_spent', op: 'gte', value: 1000, within_days: null, category: null })
  })
})

describe('addableKinds', () => {
  it('offers each attribute once but lets behaviours and metrics repeat', () => {
    const kinds = addableKinds(example.conditions, example, options)
    expect(kinds).not.toContain('city')
    expect(kinds).toEqual(['language', 'gender', 'loyalty_tier', 'age', 'event', 'signup', 'metric'])
  })

  it('offers nothing once the rule is full', () => {
    const full: Rule = { match: 'all', conditions: Array.from({ length: 10 }, () => defaultCondition('event', options)) }
    expect(addableKinds(full.conditions, full, options)).toEqual([])
  })
})

describe('normaliseRule', () => {
  it('sends "everyone" without the conditions left in the draft', () => {
    expect(normaliseRule({ ...example, everyone: true })).toEqual({ match: example.match, conditions: [], everyone: true })
  })

  it('leaves the flag out of every other rule', () => {
    expect('everyone' in normaliseRule({ ...example, everyone: false })).toBe(false)
  })

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
        { type: 'event', event: 'purchase', category: '' as unknown as null, within_days: 7, exists: true, min_count: 1, max_count: null },
        { type: 'event', event: 'cart_add', category: null, within_days: 7, exists: false, min_count: 5, max_count: null },
      ],
    }
    expect(normaliseRule(rule).conditions).toEqual([
      { type: 'event', event: 'purchase', category: null, within_days: 7, exists: true, min_count: null, max_count: null },
      { type: 'event', event: 'cart_add', category: null, within_days: 7, exists: false, min_count: null, max_count: null },
    ])
  })

  it('keeps "all time" as null for metrics and truncates decimals', () => {
    const rule: Rule = { match: 'any', conditions: [{ type: 'metric', metric: 'order_count', op: 'lte', value: 2.9, within_days: null, category: null }] }
    expect(normaliseRule(rule).conditions[0]).toEqual({ type: 'metric', metric: 'order_count', op: 'lte', value: 2, within_days: null, category: null })
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

describe('groups', () => {
  const grouped: Rule = {
    match: 'all',
    conditions: [
      { type: 'attribute', field: 'city', op: 'in', values: ['İstanbul'] },
      {
        type: 'group',
        match: 'any',
        conditions: [
          { type: 'event', event: 'cart_add', category: null, within_days: 30, exists: true, min_count: null, max_count: null },
          { type: 'metric', metric: 'total_spent', op: 'gte', value: 1000, within_days: null, category: null },
        ],
      },
    ],
  }

  it('counts the plain conditions inside groups', () => {
    expect(leafCount(grouped)).toBe(3)
    expect(leafCount(example)).toBe(3)
  })

  it('starts a group with two members and the connector the rule does not use', () => {
    const group = defaultGroup(example, options)
    expect(group.match).toBe('any')
    expect(group.conditions).toHaveLength(2)
    expect(defaultGroup({ match: 'any', conditions: [] }, options).match).toBe('all')
  })

  it('offers kinds per list: a city in the rule does not use up the city of a group', () => {
    const group = grouped.conditions[1]
    if (group?.type !== 'group') throw new Error('not a group')
    expect(addableKinds(grouped.conditions, grouped, options)).not.toContain('city')
    expect(addableKinds(group.conditions, grouped, options)).toContain('city')
  })

  it('stops offering conditions and groups when the rule is full, groups included', () => {
    const nine: Rule = { match: 'all', conditions: [...Array.from({ length: 7 }, () => defaultCondition('event', options)), defaultGroup(example, options)] }
    expect(leafCount(nine)).toBe(9)
    expect(canAddGroup(nine, options)).toBe(false) // a group needs room for two
    expect(addableKinds(nine.conditions, nine, options)).not.toEqual([])
    nine.conditions.push(defaultCondition('signup', options))
    expect(addableKinds(nine.conditions, nine, options)).toEqual([])
  })

  it('normalises members and keeps a group opposite to the rule', () => {
    const draft = cloneRule(grouped)
    const group = draft.conditions[1]
    if (group?.type !== 'group') throw new Error('not a group')
    // What the inputs hold while typing.
    ;(group.conditions[0] as unknown as { within_days: unknown }).within_days = '14'
    ;(group.conditions[1] as unknown as { value: unknown }).value = ''
    // The marketer switches the rule from "all" to "any": the group follows.
    draft.match = 'any'

    const sent = normaliseRule(draft)
    const out = sent.conditions[1]
    if (out?.type !== 'group') throw new Error('not a group')
    expect(out.match).toBe('all')
    expect(out.conditions[0]).toMatchObject({ within_days: 14 })
    expect(out.conditions[1]).toMatchObject({ value: 0 })
    expect(normaliseRule(grouped)).toEqual(grouped)
    expect(sameRule(draft, grouped)).toBe(false)
  })

  it('places backend errors on the member or on the group they are about', () => {
    const details = [
      { path: 'conditions[1].conditions[0].within_days', message: 'gün geçersiz' },
      { path: 'conditions[1].conditions[1]', message: 'çelişiyor' },
      { path: 'conditions[1].conditions', message: 'en az 2 koşul' },
      { path: 'conditions[1].match', message: 'aynı bağlaç' },
      { path: 'conditions[0].values[0]', message: 'şehir' },
    ]
    expect(memberErrors(details, 1, 0)).toEqual(['gün geçersiz'])
    expect(memberErrors(details, 1, 1)).toEqual(['çelişiyor'])
    expect(groupErrors(details, 1)).toEqual(['en az 2 koşul', 'aynı bağlaç'])
    expect(errorsFor(details, 0)).toEqual(['şehir'])
    expect(generalErrors(details)).toEqual([])
  })
})

describe('searchOptions', () => {
  const cities = ['İstanbul', 'Ankara', 'İzmir', 'Ağrı', 'Iğdır', 'Şanlıurfa', 'Kahramanmaraş', 'Karaman'].map((c) => ({ value: c, label: c }))
  const labels = (query: string, chosen: string[] = []) => searchOptions(cities, query, chosen).map((o) => o.label)

  it('offers everything not chosen yet when nothing is typed', () => {
    expect(labels('', ['Ankara'])).toEqual(['İstanbul', 'İzmir', 'Ağrı', 'Iğdır', 'Şanlıurfa', 'Kahramanmaraş', 'Karaman'])
  })

  it('ignores case and Turkish letters', () => {
    expect(labels('sanli')).toEqual(['Şanlıurfa'])
    expect(labels('IZ')).toEqual(['İzmir'])
    expect(labels('ığ')).toEqual(['Iğdır'])
    expect(labels('agri')).toEqual(['Ağrı'])
  })

  it('puts names that start with the query first', () => {
    expect(labels('mar')).toEqual(['Kahramanmaraş'])
    expect(labels('kar')).toEqual(['Karaman', 'Ankara'])
    expect(labels('an')).toEqual(['Ankara', 'İstanbul', 'Şanlıurfa', 'Kahramanmaraş', 'Karaman'])
  })

  it('leaves out what is already chosen', () => {
    expect(labels('i', ['İstanbul'])).toEqual(['İzmir', 'Iğdır', 'Ağrı', 'Şanlıurfa'])
  })
})

describe('counts and categories', () => {
  it('keeps an upper bound and drops it with "did not"', () => {
    const rule: Rule = {
      match: 'all',
      conditions: [
        { type: 'event', event: 'purchase', category: null, within_days: 90, exists: true, min_count: null, max_count: '1' as unknown as number },
        { type: 'event', event: 'cart_add', category: null, within_days: 30, exists: false, min_count: null, max_count: 4 },
        { type: 'event', event: 'cart_add', category: null, within_days: 7, exists: true, min_count: null, max_count: '' as unknown as null },
      ],
    }
    expect(normaliseRule(rule).conditions.map((c) => (c as { max_count: number | null }).max_count)).toEqual([1, null, null])
  })

  it('sends an empty metric category as "all categories"', () => {
    const rule: Rule = {
      match: 'all',
      conditions: [
        { type: 'metric', metric: 'avg_order_value', op: 'gte', value: 500, within_days: null, category: '' as unknown as null },
        { type: 'metric', metric: 'total_spent', op: 'gte', value: 500, within_days: null, category: 'books' },
      ],
    }
    expect(normaliseRule(rule).conditions.map((c) => (c as { category: string | null }).category)).toEqual([null, 'books'])
  })
})
