import { describe, expect, it } from 'vitest'
import { otherTheme, parseTheme } from './theme'

describe('theme', () => {
  it('accepts only the two themes from storage', () => {
    expect(parseTheme('dark')).toBe('dark')
    expect(parseTheme('light')).toBe('light')
    expect(parseTheme('blue')).toBeNull()
    expect(parseTheme(null)).toBeNull()
  })

  it('switches between light and dark', () => {
    expect(otherTheme('light')).toBe('dark')
    expect(otherTheme('dark')).toBe('light')
  })
})
