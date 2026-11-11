import { describe, expect, it } from 'vitest'
import { countChars, measure } from './template'

const variables = { first_name: 10, last_name: 12, city: 9 }

describe('countChars', () => {
  it('counts code points like the backend counts runes', () => {
    expect(countChars('şğüöçİı')).toBe(7)
    expect(countChars('🎉')).toBe(1) // two UTF-16 units, one character
    expect('🎉'.length).toBe(2)
  })
})

// These cases mirror backend/internal/content/content_test.go: the counter in
// the browser must agree with the validator on the server.
describe('measure', () => {
  it('counts each variable at its longest value', () => {
    const sms = 'Merhaba {{first_name}}, %30 indirim seni bekliyor!'
    expect(measure(sms, variables)).toEqual({ length: 46, problems: [] })
  })

  it('counts a repeated variable each time', () => {
    expect(measure('{{first_name}} {{first_name}}', variables).length).toBe(21)
  })

  it('is exactly at the limit when the backend says so', () => {
    const fixed = countChars('Merhaba , ')
    const sms = 'Merhaba {{first_name}}, ' + 'ş'.repeat(160 - fixed - 10)
    expect(measure(sms, variables).length).toBe(160)
    expect(measure(sms, { ...variables, first_name: 11 }).length).toBe(161)
  })

  it('accepts spaces inside the braces and single braces in text', () => {
    expect(measure('Merhaba {{ first_name }}, {kod} }', variables).problems).toEqual([])
  })

  it.each([
    ['Merhaba {{name}}', 'bilinmeyen bir değişken'],
    ['Merhaba {{user.first_name}}', 'geçerli bir değişken adı değil'],
    ['Merhaba {{FIRST_NAME}}', 'geçerli bir değişken adı değil'],
    ['Merhaba {{}}', 'geçerli bir değişken adı değil'],
    ['Merhaba {{first_name', 'kapanmamış'],
    ['Merhaba {{ {{first_name}} }}', 'kapanmamış'],
    ['Merhaba first_name}}', 'karşılığı olmayan'],
    ['   ', 'boş olamaz'],
  ])('reports a problem for %j', (template, problem) => {
    const { problems } = measure(template, variables)
    expect(problems.join(' | ')).toContain(problem)
  })

  it('names the allowed variables', () => {
    expect(measure('{{name}}', variables).problems[0]).toContain('{{first_name}}, {{last_name}}, {{city}}')
  })
})
