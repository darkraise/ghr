import { act, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { json, mockApi } from "@/test/api"
import { setUnauthorizedHandler } from "./client"
import { readChanges, RETRY_MAX, RETRY_MIN, runChanges, setLive, SseParser, useLive } from "./live"

const enc = new TextEncoder()

function recorder() {
  const seen: string[] = []
  return {
    seen,
    handlers: {
      onHello: () => {
        seen.push("hello")
      },
      onChange: (topics: string[]) => {
        seen.push(`change ${topics.join(",")}`)
      },
      onAlive: () => {
        seen.push("alive")
      },
    },
  }
}

function stream(chunks: (string | Uint8Array)[]): Response {
  return new Response(
    new ReadableStream<Uint8Array>({
      start(c) {
        for (const ch of chunks) c.enqueue(typeof ch === "string" ? enc.encode(ch) : ch)
        c.close()
      },
    }),
    { status: 200, headers: { "Content-Type": "text/event-stream" } },
  )
}

const noop = { onHello: () => {}, onChange: () => {}, onEnd: () => {} }

afterEach(() => {
  setUnauthorizedHandler(() => {})
  setLive(false)
})

describe("SseParser", () => {
  it("joins an event split across chunks", () => {
    const { seen, handlers } = recorder()
    const p = new SseParser(handlers)
    p.push("event: cha")
    p.push('nge\ndata: {"topics":["sta')
    p.push('tus"]}\n')
    p.push("\n")
    expect(seen).toEqual(["change status"])
  })

  it("reads two events in one chunk with CRLF line endings", () => {
    const { seen, handlers } = recorder()
    new SseParser(handlers).push('event: hello\r\ndata: {}\r\n\r\nevent: change\r\ndata: {"topics":["config","status"]}\r\n\r\n')
    expect(seen).toEqual(["hello", "change config,status"])
  })

  it("calls onAlive for a comment line", () => {
    const { seen, handlers } = recorder()
    new SseParser(handlers).push(": ping\n\n")
    expect(seen).toEqual(["alive"])
  })

  it("strips one leading space and joins data lines with LF", () => {
    const { seen, handlers } = recorder()
    new SseParser(handlers).push('event: change\ndata: {"topics":\ndata:["status"]}\n\n')
    expect(seen).toEqual(["change status"])
  })

  it("ignores unknown events and unparseable change data", () => {
    const { seen, handlers } = recorder()
    new SseParser(handlers).push('event: other\ndata: {}\n\nevent: change\ndata: not json\n\nevent: change\ndata: {"topics":[1]}\n\n')
    expect(seen).toEqual([])
  })
})

describe("readChanges", () => {
  it("decodes a character split across chunks and sends X-GHR", async () => {
    const bytes = enc.encode('event: change\ndata: {"topics":["café"]}\n\n')
    const cut = bytes.indexOf(0xc3) + 1
    const { fetchMock } = mockApi({ "GET /api/changes": () => stream([bytes.slice(0, cut), bytes.slice(cut)]) })
    const { seen, handlers } = recorder()
    expect(await readChanges(handlers, new AbortController().signal)).toBe("ended")
    expect(seen).toEqual(["change café"])
    expect(new Headers(fetchMock.mock.calls[0]?.[1]?.headers).get("X-GHR")).toBe("1")
  })

  it("reports a 401 to the unauthorized handler", async () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    mockApi({ "GET /api/changes": () => json({ error: "not logged in" }, 401) })
    expect(await readChanges(recorder().handlers, new AbortController().signal)).toBe("unauthorized")
    expect(onUnauthorized).toHaveBeenCalledOnce()
  })

  it("ends a 503 without reading it as events", async () => {
    mockApi({ "GET /api/changes": () => json({ error: "not ready" }, 503) })
    const { seen, handlers } = recorder()
    expect(await readChanges(handlers, new AbortController().signal)).toBe("ended")
    expect(seen).toEqual([])
  })

  it("cancels the reader on abort", async () => {
    let cancelled = false
    mockApi({
      "GET /api/changes": () =>
        new Response(
          new ReadableStream<Uint8Array>({
            start(c) {
              c.enqueue(enc.encode("event: hello\ndata: {}\n\n"))
            },
            cancel() {
              cancelled = true
            },
          }),
        ),
    })
    const ctrl = new AbortController()
    const { handlers } = recorder()
    const end = await readChanges({ ...handlers, onHello: () => ctrl.abort() }, ctrl.signal)
    expect(end).toBe("ended")
    expect(cancelled).toBe(true)
  })
})

describe("runChanges", () => {
  it("backs off from 1 s, doubling to 30 s, ending each connection", async () => {
    mockApi({ "GET /api/changes": () => json({ error: "not ready" }, 503) })
    const ctrl = new AbortController()
    const waits: number[] = []
    let ends = 0
    const wait = async (ms: number) => {
      waits.push(ms)
      if (waits.length === 7) ctrl.abort()
    }
    await runChanges({ ...noop, onEnd: () => ends++ }, ctrl.signal, wait)
    expect(waits).toEqual([RETRY_MIN, 2000, 4000, 8000, 16000, RETRY_MAX, RETRY_MAX])
    expect(ends).toBe(7)
  })

  it("resets the delay only after a change or a ping", async () => {
    const answers = [
      () => json({ error: "not ready" }, 503),
      () => json({ error: "not ready" }, 503),
      () => stream(["event: hello\ndata: {}\n\n"]),
      () => stream(["event: hello\ndata: {}\n\n: ping\n\n"]),
      () => stream(['event: hello\ndata: {}\n\nevent: change\ndata: {"topics":["status"]}\n\n']),
    ]
    let n = 0
    mockApi({ "GET /api/changes": () => (answers[n++] ?? (() => json({ error: "not ready" }, 503)))() })
    const ctrl = new AbortController()
    const waits: number[] = []
    const wait = async (ms: number) => {
      waits.push(ms)
      if (waits.length === 5) ctrl.abort()
    }
    await runChanges(noop, ctrl.signal, wait)
    expect(waits).toEqual([1000, 2000, 4000, 1000, 1000])
  })

  it("stops after a 401", async () => {
    const { calls } = mockApi({ "GET /api/changes": () => json({ error: "not logged in" }, 401) })
    const waits: number[] = []
    await runChanges(noop, new AbortController().signal, async (ms) => {
      waits.push(ms)
    })
    expect(waits).toEqual([])
    expect(calls).toHaveLength(1)
  })

  it("does not reconnect after an abort", async () => {
    const { calls } = mockApi({
      "GET /api/changes": () =>
        new Response(
          new ReadableStream<Uint8Array>({
            start(c) {
              c.enqueue(enc.encode("event: hello\ndata: {}\n\n"))
            },
          }),
        ),
    })
    const ctrl = new AbortController()
    const waits: number[] = []
    await runChanges({ ...noop, onHello: () => ctrl.abort() }, ctrl.signal, async (ms) => {
      waits.push(ms)
    })
    expect(waits).toEqual([])
    expect(calls).toHaveLength(1)
  })
})

describe("useLive", () => {
  it("follows setLive", () => {
    const { result } = renderHook(() => useLive())
    expect(result.current).toBe(false)
    act(() => setLive(true))
    expect(result.current).toBe(true)
    act(() => setLive(false))
    expect(result.current).toBe(false)
  })
})
