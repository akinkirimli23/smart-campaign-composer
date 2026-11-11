// Server-sent events over fetch. The browser's EventSource cannot POST, and
// content generation needs a request body, so the stream is parsed here.

export interface SSEMessage {
  event: string
  data: string
}

/**
 * Incremental parser for the text/event-stream format. Feed it chunks as
 * they arrive; it returns the events completed by each chunk. A chunk may
 * end anywhere, including in the middle of a line.
 */
export class SSEParser {
  private buffer = ''

  push(chunk: string): SSEMessage[] {
    this.buffer += chunk.replace(/\r\n?/g, '\n')
    const messages: SSEMessage[] = []
    for (;;) {
      const end = this.buffer.indexOf('\n\n')
      if (end < 0) break
      const block = this.buffer.slice(0, end)
      this.buffer = this.buffer.slice(end + 2)
      const message = parseBlock(block)
      if (message) messages.push(message)
    }
    return messages
  }
}

function parseBlock(block: string): SSEMessage | null {
  let event = 'message'
  const data: string[] = []
  for (const line of block.split('\n')) {
    if (line === '' || line.startsWith(':')) continue // blank or comment
    const colon = line.indexOf(':')
    const field = colon < 0 ? line : line.slice(0, colon)
    let value = colon < 0 ? '' : line.slice(colon + 1)
    if (value.startsWith(' ')) value = value.slice(1)
    if (field === 'event') event = value
    else if (field === 'data') data.push(value)
  }
  return data.length > 0 ? { event, data: data.join('\n') } : null
}

/** Reads a response body as an event stream, calling onMessage per event. */
export async function readEventStream(
  body: ReadableStream<Uint8Array>,
  onMessage: (message: SSEMessage) => void,
): Promise<void> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  const parser = new SSEParser()
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    // stream: true keeps a multi-byte character split across chunks intact.
    for (const message of parser.push(decoder.decode(value, { stream: true }))) onMessage(message)
  }
}
