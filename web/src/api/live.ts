import { useSyncExternalStore } from "react"
import { notifyUnauthorized } from "./client"

// Tests turn the stream off so their fetch mocks see only the queries.
export const liveConfig = { enabled: true }

export const RETRY_MIN = 1000
export const RETRY_MAX = 30_000

export interface ChangeHandlers {
  onHello: () => void
  onChange: (topics: string[]) => void
  onAlive: () => void
}

// SseParser reads the server-sent events of GET /changes from decoded text
// that may split anywhere, a line or a field included.
export class SseParser {
  private readonly h: ChangeHandlers
  private buf = ""
  private event = ""
  private data: string[] = []

  constructor(h: ChangeHandlers) {
    this.h = h
  }

  push(text: string): void {
    this.buf += text
    for (;;) {
      const i = this.buf.indexOf("\n")
      if (i < 0) return
      let line = this.buf.slice(0, i)
      this.buf = this.buf.slice(i + 1)
      if (line.endsWith("\r")) line = line.slice(0, -1)
      this.line(line)
    }
  }

  private line(line: string): void {
    if (line === "") {
      this.dispatch()
      return
    }
    if (line.startsWith(":")) {
      this.h.onAlive()
      return
    }
    const colon = line.indexOf(":")
    const field = colon < 0 ? line : line.slice(0, colon)
    let value = colon < 0 ? "" : line.slice(colon + 1)
    if (value.startsWith(" ")) value = value.slice(1)
    if (field === "event") this.event = value
    else if (field === "data") this.data.push(value)
  }

  private dispatch(): void {
    const event = this.event
    const data = this.data.join("\n")
    this.event = ""
    this.data = []
    if (event === "hello") {
      this.h.onHello()
    } else if (event === "change") {
      let topics: unknown
      try {
        topics = (JSON.parse(data) as { topics?: unknown }).topics
      } catch {
        return
      }
      if (Array.isArray(topics) && topics.every((t) => typeof t === "string")) this.h.onChange(topics)
    }
  }
}

export type StreamEnd = "ended" | "unauthorized"

// The server writes a ping every 15 seconds while it has nothing to say, so
// this much silence means the connection is dead although it looks open.
const STREAM_SILENCE = 45_000

// readChanges reads one connection to the change stream until it ends. An
// abort through signal, or STREAM_SILENCE without a byte, cancels the reader
// and ends it like any other end.
export async function readChanges(h: ChangeHandlers, signal: AbortSignal): Promise<StreamEnd> {
  const attempt = new AbortController()
  const stop = () => attempt.abort()
  signal.addEventListener("abort", stop)
  if (signal.aborted) stop()
  let watchdog = setTimeout(stop, STREAM_SILENCE)
  const heard = () => {
    clearTimeout(watchdog)
    watchdog = setTimeout(stop, STREAM_SILENCE)
  }
  try {
    return await readAttempt(h, attempt.signal, heard)
  } finally {
    clearTimeout(watchdog)
    signal.removeEventListener("abort", stop)
  }
}

async function readAttempt(h: ChangeHandlers, signal: AbortSignal, heard: () => void): Promise<StreamEnd> {
  let res: Response
  try {
    res = await fetch("/api/changes", { headers: { "X-GHR": "1" }, credentials: "same-origin", signal })
  } catch {
    return "ended"
  }
  heard()
  if (res.status === 401) {
    notifyUnauthorized()
    return "unauthorized"
  }
  if (!res.ok || !res.body) {
    void res.body?.cancel()
    return "ended"
  }
  const reader = res.body.getReader()
  const cancel = () => {
    reader.cancel().catch(() => {})
  }
  signal.addEventListener("abort", cancel)
  if (signal.aborted) cancel()
  const decoder = new TextDecoder()
  const parser = new SseParser(h)
  try {
    for (;;) {
      let chunk: ReadableStreamReadResult<Uint8Array>
      try {
        chunk = await reader.read()
      } catch {
        // a dropped connection ends the stream like a clean end
        break
      }
      if (chunk.done) break
      heard()
      try {
        parser.push(decoder.decode(chunk.value, { stream: true }))
      } catch (err) {
        // End the connection so onEnd clears the live flag; a bug in a
        // handler must not leave the polling slowed with no stream behind it.
        console.error("change stream handler failed", err)
        cancel()
        break
      }
    }
  } finally {
    signal.removeEventListener("abort", cancel)
  }
  return "ended"
}

export interface StreamHandlers {
  onHello: () => void
  onChange: (topics: string[]) => void
  // onEnd runs after every connection ends, before any wait to reconnect.
  onEnd: () => void
}

export function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const done = () => {
      clearTimeout(timer)
      signal.removeEventListener("abort", done)
      resolve()
    }
    const timer = setTimeout(done, ms)
    signal.addEventListener("abort", done)
  })
}

// runChanges keeps the change stream open until signal aborts or the session
// ends. The delay resets only after a connection delivered a change or a
// ping, so a proxy that drops right after hello cannot cause a 1 s loop.
export async function runChanges(h: StreamHandlers, signal: AbortSignal, wait = sleep): Promise<void> {
  let delay = RETRY_MIN
  while (!signal.aborted) {
    const conn = { alive: false }
    const end = await readChanges(
      {
        onHello: h.onHello,
        onChange: (topics) => {
          conn.alive = true
          h.onChange(topics)
        },
        onAlive: () => {
          conn.alive = true
        },
      },
      signal,
    )
    h.onEnd()
    if (end === "unauthorized" || signal.aborted) return
    if (conn.alive) delay = RETRY_MIN
    await wait(delay, signal)
    delay = Math.min(delay * 2, RETRY_MAX)
  }
}

let live = false
const listeners = new Set<() => void>()

export function setLive(v: boolean): void {
  if (live === v) return
  live = v
  for (const l of listeners) l()
}

function subscribe(l: () => void): () => void {
  listeners.add(l)
  return () => {
    listeners.delete(l)
  }
}

// useLive is true from a hello until the stream ends or the tab is hidden.
export function useLive(): boolean {
  return useSyncExternalStore(subscribe, () => live, () => false)
}
