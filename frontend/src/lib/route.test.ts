import { describe, expect, it } from 'vitest'
import { formatHash, parseHash, sameRoute, type Route } from './route'

describe('route', () => {
  const routes: Route[] = [{ name: 'compose' }, { name: 'campaigns' }, { name: 'campaign', id: 12 }]

  it('round-trips every route through the hash', () => {
    for (const route of routes) expect(parseHash(formatHash(route))).toEqual(route)
    expect(formatHash({ name: 'campaign', id: 12 })).toBe('#/kampanya/12')
    expect(formatHash({ name: 'compose' })).toBe('')
  })

  it.each(['', '#', '#/', '#/kampanya/', '#/kampanya/0', '#/kampanya/-3', '#/kampanya/12abc', '#/kampanya/1.5', '#/başka', '#/kampanyalar/7'])(
    'falls back to the composer for %j',
    (hash) => {
      expect(parseHash(hash)).toEqual({ name: 'compose' })
    },
  )

  it('compares routes by where they lead', () => {
    expect(sameRoute({ name: 'campaign', id: 3 }, { name: 'campaign', id: 3 })).toBe(true)
    expect(sameRoute({ name: 'campaign', id: 3 }, { name: 'campaign', id: 4 })).toBe(false)
    expect(sameRoute({ name: 'campaigns' }, { name: 'compose' })).toBe(false)
  })
})
