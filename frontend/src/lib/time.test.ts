import { describe, expect, it } from 'vitest'
import { fromLocalInput, toLocalInput } from './time'

describe('datetime-local conversions', () => {
  it('round-trips an instant through the input format, to the minute', () => {
    const at = new Date(2026, 10, 14, 9, 5, 42) // 14 Nov 2026 09:05:42 local
    expect(toLocalInput(at)).toBe('2026-11-14T09:05')
    expect(fromLocalInput('2026-11-14T09:05')?.getTime()).toBe(new Date(2026, 10, 14, 9, 5, 0).getTime())
  })

  it('pads single digits', () => {
    expect(toLocalInput(new Date(2027, 0, 3, 0, 0))).toBe('2027-01-03T00:00')
  })

  it('reports an empty or meaningless value as no date', () => {
    expect(fromLocalInput('')).toBeNull()
    expect(fromLocalInput('yarın sabah')).toBeNull()
  })
})
