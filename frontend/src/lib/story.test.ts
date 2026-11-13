import { describe, expect, it } from 'vitest'
import { activeChapter, chapterPhase, CHAPTERS, sceneAt, scrollProgress, smoothstep, typedCount } from './story'

describe('scroll story', () => {
  it('maps the scroll position of the pinned section to 0..1', () => {
    // A 5000px section in a 1000px viewport scrolls for 4000px.
    expect(scrollProgress(200, 5000, 1000)).toBe(0) // not reached yet
    expect(scrollProgress(0, 5000, 1000)).toBe(0)
    expect(scrollProgress(-1000, 5000, 1000)).toBe(0.25)
    expect(scrollProgress(-4000, 5000, 1000)).toBe(1)
    expect(scrollProgress(-9000, 5000, 1000)).toBe(1) // scrolled past
    expect(scrollProgress(-100, 800, 1000)).toBe(0) // nothing to scroll through
  })

  it('eases between its edges and holds outside them', () => {
    expect(smoothstep(0.2, 0.4, 0.1)).toBe(0)
    expect(smoothstep(0.2, 0.4, 0.3)).toBeCloseTo(0.5, 10)
    expect(smoothstep(0.2, 0.4, 0.9)).toBe(1)
  })

  it('shows one message at a time: a chapter has left before the next arrives', () => {
    for (let i = 0; i <= 1000; i++) {
      const p = i / 1000
      const visible = CHAPTERS.filter((c) => {
        const { enter, exit } = chapterPhase(p, c)
        return enter > 0 && exit < 1
      })
      expect(visible.length, `at p=${p}`).toBeLessThanOrEqual(1)
    }
  })

  it('opens on the first message and ends on the last', () => {
    expect(chapterPhase(0, CHAPTERS[0]!)).toEqual({ enter: 1, exit: 0 })
    const last = CHAPTERS[CHAPTERS.length - 1]!
    expect(chapterPhase(1, last)).toEqual({ enter: 1, exit: 0 })
    expect(activeChapter(0)).toBe(0)
    expect(activeChapter(0.5)).toBe(2)
    expect(activeChapter(1)).toBe(CHAPTERS.length - 1)
  })

  it('is a function of the position alone, so scrolling back rewinds it', () => {
    const forward = [0.1, 0.3, 0.5, 0.7, 0.9].map(sceneAt)
    const backward = [0.9, 0.7, 0.5, 0.3, 0.1].map(sceneAt).reverse()
    expect(backward).toEqual(forward)
  })

  it('moves each scene only while its own message is on screen', () => {
    const at = (id: string) => CHAPTERS.find((c) => c.id === id)!
    // Before a chapter starts its scene has not begun; after it ends, it is complete.
    expect(sceneAt(at('brief').from).type).toBe(0)
    expect(sceneAt(at('brief').to).type).toBe(1)
    expect(sceneAt(at('audience').from).select).toBe(0)
    expect(sceneAt(at('audience').to).gather).toBe(1)
    expect(sceneAt(at('message').from).card).toBe(0)
    expect(sceneAt(at('message').to).card).toBe(1)
    expect(sceneAt(at('send').from).send).toBe(0)
    expect(sceneAt(1).send).toBe(1)
  })

  it('never goes backwards while scrolling forwards', () => {
    let previous = sceneAt(0)
    for (let i = 1; i <= 200; i++) {
      const now = sceneAt(i / 200)
      for (const key of ['type', 'select', 'gather', 'card', 'send'] as const) {
        expect(now[key]).toBeGreaterThanOrEqual(previous[key])
      }
      previous = now
    }
  })

  it('types whole characters', () => {
    expect(typedCount(0, 80)).toBe(0)
    expect(typedCount(0.5, 80)).toBe(40)
    expect(typedCount(1.4, 80)).toBe(80)
  })
})
