import { describe, expect, it } from 'vitest'
import { readEventStream, SSEParser, type SSEMessage } from './sse'

const stream =
  'event: attempt\ndata: {"attempt":1}\n\n' +
  'event: delta\ndata: {"field":"sms","text":"Merhaba Ayşe 🎉"}\n\n' +
  'event: result\ndata: {"ok":true}\n\n'

const expected: SSEMessage[] = [
  { event: 'attempt', data: '{"attempt":1}' },
  { event: 'delta', data: '{"field":"sms","text":"Merhaba Ayşe 🎉"}' },
  { event: 'result', data: '{"ok":true}' },
]

describe('SSEParser', () => {
  it('parses the same events however the text is chunked', () => {
    for (let size = 1; size <= stream.length; size++) {
      const parser = new SSEParser()
      const got: SSEMessage[] = []
      for (let i = 0; i < stream.length; i += size) got.push(...parser.push(stream.slice(i, i + size)))
      expect(got, `chunk size ${size}`).toEqual(expected)
    }
  })

  it('holds an incomplete event until its blank line arrives', () => {
    const parser = new SSEParser()
    expect(parser.push('event: delta\ndata: {"a":1}\n')).toEqual([])
    expect(parser.push('\n')).toEqual([{ event: 'delta', data: '{"a":1}' }])
  })

  it('handles CRLF, comments, a default event name and multi-line data', () => {
    const parser = new SSEParser()
    expect(parser.push(': keep-alive\r\n\r\ndata: one\r\ndata: two\r\n\r\n')).toEqual([{ event: 'message', data: 'one\ntwo' }])
  })
})

describe('readEventStream', () => {
  it('keeps characters intact when a chunk boundary splits their bytes', async () => {
    const bytes = new TextEncoder().encode(stream)
    for (const size of [1, 2, 3, 7]) {
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          for (let i = 0; i < bytes.length; i += size) controller.enqueue(bytes.slice(i, i + size))
          controller.close()
        },
      })
      const got: SSEMessage[] = []
      await readEventStream(body, (m) => got.push(m))
      expect(got, `chunk size ${size}`).toEqual(expected)
    }
  })
})
