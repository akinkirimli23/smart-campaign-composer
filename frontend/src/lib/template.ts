// Client-side mirror of the backend's template check (internal/content).
// It exists for instant feedback while typing: the live character counter and
// the red border. The backend re-validates everything on approval and is the
// only authority; if the two ever disagree, the backend wins.

export interface Measured {
  /** Length with every variable at its longest value, in characters. */
  length: number
  /** Problems that make the template invalid (unknown variable, bad braces). */
  problems: string[]
}

const NAME = /^[a-z][a-z0-9_]*$/

/** Counts characters the way the backend does: code points, not UTF-16 units. */
export function countChars(text: string): number {
  return Array.from(text).length
}

/**
 * Measures a template against the longest value of each variable.
 * `variables` comes from the backend ({ first_name: 7, ... }).
 */
export function measure(template: string, variables: Record<string, number>): Measured {
  const problems: string[] = []
  const allowed = Object.keys(variables)
    .map((name) => `{{${name}}}`)
    .join(', ')
  let length = 0
  let rest = template

  for (;;) {
    const open = rest.indexOf('{{')
    if (open < 0) break
    const literal = rest.slice(0, open)
    if (literal.includes('}}')) problems.push('karşılığı olmayan "}}" var')
    length += countChars(literal)
    rest = rest.slice(open + 2)

    const close = rest.indexOf('}}')
    if (close < 0 || rest.slice(0, close).includes('{{')) {
      problems.push('kapanmamış "{{" var; değişkenler {{ad}} biçiminde yazılır')
      return { length: length + countChars(rest) + 2, problems }
    }
    const name = rest.slice(0, close).trim()
    rest = rest.slice(close + 2)
    if (!NAME.test(name)) {
      problems.push(`{{${name}}} geçerli bir değişken adı değil; izinli değişkenler: ${allowed}`)
    } else if (!(name in variables)) {
      problems.push(`{{${name}}} bilinmeyen bir değişken; izinli değişkenler: ${allowed}`)
    } else {
      length += variables[name] ?? 0
    }
  }
  if (rest.includes('}}')) problems.push('karşılığı olmayan "}}" var')
  length += countChars(rest)

  if (template.trim() === '') problems.push('boş olamaz')
  return { length, problems }
}
