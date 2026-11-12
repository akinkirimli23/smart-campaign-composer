// Conversions between instants and what <input type="datetime-local"> holds:
// a wall-clock time in the browser's zone, to the minute, e.g. "2026-11-14T09:30".

const pad = (n: number) => String(n).padStart(2, '0')

/** An instant as a datetime-local value in the browser's time zone. */
export function toLocalInput(at: Date): string {
  return `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}T${pad(at.getHours())}:${pad(at.getMinutes())}`
}

/** A datetime-local value as an instant, or null when it is empty or not a date. */
export function fromLocalInput(value: string): Date | null {
  if (value === '') return null
  const at = new Date(value) // parsed in the browser's time zone
  return Number.isNaN(at.getTime()) ? null : at
}

/** The browser's IANA time zone ("Europe/Istanbul"), or "" when unknown. */
export function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone ?? ''
  } catch {
    return ''
  }
}

/** "13 Kasım 2026 09:00", for showing a send time to the marketer. */
export function formatDateTime(at: Date): string {
  return at.toLocaleString('tr-TR', { dateStyle: 'long', timeStyle: 'short' })
}
