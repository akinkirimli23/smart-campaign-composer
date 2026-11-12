// Light / dark theme. Without a stored choice the page follows the system
// setting (plain CSS, see style.css); the header switch stores a choice and
// puts it on <html data-theme>.

export type Theme = 'light' | 'dark'

const KEY = 'scc-theme'

export function parseTheme(value: unknown): Theme | null {
  return value === 'light' || value === 'dark' ? value : null
}

/** The visitor's stored choice, if any. Storage can be unavailable (private mode). */
export function storedTheme(): Theme | null {
  try {
    return parseTheme(localStorage.getItem(KEY))
  } catch {
    return null
  }
}

export function systemTheme(): Theme {
  return typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function otherTheme(theme: Theme): Theme {
  return theme === 'dark' ? 'light' : 'dark'
}

/** Applies a choice to the page and remembers it. */
export function chooseTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme
  try {
    localStorage.setItem(KEY, theme)
  } catch {
    // not remembered; the page still switches
  }
}

/** Applies the stored choice on page load; no choice leaves it to the system. */
export function restoreTheme() {
  const stored = storedTheme()
  if (stored) document.documentElement.dataset.theme = stored
}
