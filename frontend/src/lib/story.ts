// The home page's scroll story as plain functions of one number: the scroll
// progress p through the pinned scene, 0 at its top and 1 at its end. The
// drawing and the typography are both derived from p, so they cannot drift
// apart, and scrolling back simply runs everything in reverse.

export function clamp01(x: number): number {
  return x > 0 ? (x > 1 ? 1 : x) : 0 // also turns -0 and NaN into 0
}

/** 0 before a, 1 after b, eased in between. */
export function smoothstep(a: number, b: number, x: number): number {
  const t = clamp01((x - a) / (b - a))
  return t * t * (3 - 2 * t)
}

export function lerp(a: number, b: number, t: number): number {
  return a + (b - a) * t
}

/** One message of the story: on screen while p is between from and to. */
export interface Chapter {
  id: 'intro' | 'brief' | 'audience' | 'message' | 'send'
  from: number
  to: number
}

// Consecutive chapters leave a small gap: one message has left before the
// next arrives, so two headlines never overlap.
export const CHAPTERS: Chapter[] = [
  { id: 'intro', from: 0, to: 0.13 },
  { id: 'brief', from: 0.15, to: 0.36 },
  { id: 'audience', from: 0.38, to: 0.58 },
  { id: 'message', from: 0.6, to: 0.79 },
  { id: 'send', from: 0.81, to: 1 },
]

/** How long, in progress, a chapter takes to come in or to leave. */
export const EDGE = 0.045

/**
 * Where a chapter is in its life: enter goes 0 -> 1 as it comes in, exit goes
 * 0 -> 1 as it leaves. The first chapter is already in at the top of the page
 * and the last one never leaves.
 */
export function chapterPhase(p: number, chapter: Chapter): { enter: number; exit: number } {
  const first = chapter.from <= 0
  const last = chapter.to >= 1
  return {
    enter: first ? 1 : smoothstep(chapter.from, chapter.from + EDGE, p),
    exit: last ? 0 : smoothstep(chapter.to - EDGE, chapter.to, p),
  }
}

/** The chapter whose window contains p, or the nearest earlier one. */
export function activeChapter(p: number): number {
  let active = 0
  CHAPTERS.forEach((c, i) => {
    if (p >= c.from) active = i
  })
  return active
}

/** What the drawing shows at p; every value runs 0 -> 1. */
export interface Scene {
  /** The brief is being typed. */
  type: number
  /** The people who match light up; the others dim. */
  select: number
  /** The matching people gather into one audience. */
  gather: number
  /** The audience rearranges into the lines of a message. */
  card: number
  /** The messages leave, one after another. */
  send: number
}

export function sceneAt(p: number): Scene {
  return {
    type: smoothstep(0.16, 0.33, p),
    select: smoothstep(0.38, 0.47, p),
    gather: smoothstep(0.43, 0.57, p),
    card: smoothstep(0.6, 0.73, p),
    send: smoothstep(0.82, 0.985, p),
  }
}

/** How many characters of a text of the given length have been typed. */
export function typedCount(progress: number, length: number): number {
  return Math.round(clamp01(progress) * length)
}

/**
 * Scroll position -> progress. top is the pinned section's distance from the
 * top of the viewport (negative once scrolled into), height its full height.
 */
export function scrollProgress(top: number, height: number, viewport: number): number {
  const distance = height - viewport
  return distance <= 0 ? 0 : clamp01(-top / distance)
}
