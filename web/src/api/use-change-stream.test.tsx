import { QueryClientProvider, type QueryClient } from "@tanstack/react-query"
import { act, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createQueryClient } from "@/query"
import { json, mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { trackQueryClient } from "@/test/query-clients"
import { keys, POLL_SLOW, useActivity, useConfig, useEvents, useHistory, useMetrics, useStatus, useStorage, useToken } from "./hooks"
import { liveConfig, RETRY_MIN, setLive, useLive } from "./live"
import { invalidateTopics, TOPIC_QUERIES, useChangeStream } from "./use-change-stream"

// One query per daemon-state view, plus a GitHub-backed one no topic touches.
const seeded = [
  keys.status,
  keys.config,
  keys.metrics,
  keys.token,
  keys.storage,
  keys.githubStorage,
  keys.activity("1h", "UTC", ""),
  keys.history("", "", ""),
  keys.events("e1"),
  keys.actions,
]

function seed(qc: QueryClient) {
  for (const k of seeded) qc.setQueryData(k, {})
}

function invalidated(qc: QueryClient): string[] {
  return qc
    .getQueryCache()
    .findAll()
    .filter((q) => q.state.isInvalidated)
    .map((q) => String(q.queryKey[0]))
    .sort()
}

const ALL = ["activity", "config", "events", "github-storage", "history", "metrics", "status", "storage", "token"]

describe("invalidateTopics", () => {
  it.each<[string, string[]]>([
    ["status", ["activity", "status"]],
    ["gauges", ["status", "token"]],
    ["events", ["events"]],
    ["config", ["activity", "config", "github-storage"]],
    ["token", ["token"]],
    ["storage", ["storage"]],
    ["github-storage", ["github-storage"]],
    ["history", ["activity", "history"]],
    ["metrics", ["activity", "metrics"]],
  ])("%s invalidates exactly its queries", (topic, want) => {
    const { queryClient } = withQuery()
    seed(queryClient)
    invalidateTopics(queryClient, [topic])
    expect(invalidated(queryClient)).toEqual(want)
  })

  it("ignores an unknown topic", () => {
    const { queryClient } = withQuery()
    seed(queryClient)
    invalidateTopics(queryClient, ["nope"])
    expect(invalidated(queryClient)).toEqual([])
  })

  it("names every topic", () => {
    expect(Object.keys(TOPIC_QUERIES).sort()).toEqual([
      "config",
      "events",
      "gauges",
      "github-storage",
      "history",
      "metrics",
      "status",
      "storage",
      "token",
    ])
  })
})

function controlled() {
  const enc = new TextEncoder()
  let ctrl: ReadableStreamDefaultController<Uint8Array> | undefined
  const response = new Response(
    new ReadableStream<Uint8Array>({
      start(c) {
        ctrl = c
      },
    }),
    { status: 200 },
  )
  return { response, send: (text: string) => ctrl?.enqueue(enc.encode(text)) }
}

let visibility: DocumentVisibilityState = "visible"

// The event bubbles to window, as the browser's does: the query library
// listens there for its refetch on focus.
function setVisibility(v: DocumentVisibilityState) {
  visibility = v
  document.dispatchEvent(new Event("visibilitychange", { bubbles: true }))
}

describe("useChangeStream", () => {
  beforeEach(() => {
    liveConfig.enabled = true
    visibility = "visible"
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => visibility })
  })

  afterEach(() => {
    liveConfig.enabled = false
    Reflect.deleteProperty(document, "visibilityState")
    setLive(false)
  })

  it("invalidates per topic, is not live while hidden, and refreshes everything after a reconnect", async () => {
    const streams = [controlled(), controlled()]
    let n = 0
    const { calls, fetchMock } = mockApi({ "GET /api/changes": () => streams[n++]?.response ?? json({ error: "not ready" }, 503) })
    const { queryClient, wrapper } = withQuery()
    seed(queryClient)
    const { result } = renderHook(
      () => {
        useChangeStream()
        return useLive()
      },
      { wrapper },
    )
    await waitFor(() => expect(calls.filter((c) => c.path === "/api/changes")).toHaveLength(1))
    streams[0]?.send("event: hello\ndata: {}\n\n")
    await waitFor(() => expect(result.current).toBe(true))
    expect(invalidated(queryClient)).toEqual([])
    streams[0]?.send('event: change\ndata: {"topics":["token"]}\n\n')
    await waitFor(() => expect(invalidated(queryClient)).toEqual(["token"]))

    act(() => setVisibility("hidden"))
    expect(result.current).toBe(false)
    expect(fetchMock.mock.calls[0]?.[1]?.signal?.aborted).toBe(true)
    seed(queryClient)
    act(() => setVisibility("visible"))
    await waitFor(() => expect(calls.filter((c) => c.path === "/api/changes")).toHaveLength(2))
    streams[1]?.send("event: hello\ndata: {}\n\n")
    await waitFor(() => expect(result.current).toBe(true))
    expect(invalidated(queryClient)).toEqual(ALL)
  })

  it("refreshes everything on the hello that follows a failed first attempt", async () => {
    vi.useFakeTimers()
    try {
      const stream = controlled()
      let n = 0
      const { calls } = mockApi({ "GET /api/changes": () => (n++ === 0 ? json({ error: "not ready" }, 503) : stream.response) })
      const { queryClient, wrapper } = withQuery()
      seed(queryClient)
      renderHook(() => useChangeStream(), { wrapper })
      await act(() => vi.advanceTimersByTimeAsync(0))
      expect(calls.filter((c) => c.path === "/api/changes")).toHaveLength(1)
      await act(() => vi.advanceTimersByTimeAsync(RETRY_MIN))
      expect(calls.filter((c) => c.path === "/api/changes")).toHaveLength(2)
      stream.send("event: hello\ndata: {}\n\n")
      await act(() => vi.advanceTimersByTimeAsync(0))
      expect(invalidated(queryClient)).toEqual(ALL)
    } finally {
      vi.useRealTimers()
    }
  })

  it("refreshes everything on the first hello of a page that loaded hidden", async () => {
    visibility = "hidden"
    const stream = controlled()
    const { calls } = mockApi({ "GET /api/changes": () => stream.response })
    const { queryClient, wrapper } = withQuery()
    seed(queryClient)
    const { result } = renderHook(
      () => {
        useChangeStream()
        return useLive()
      },
      { wrapper },
    )
    expect(calls).toHaveLength(0)
    act(() => setVisibility("visible"))
    await waitFor(() => expect(calls.filter((c) => c.path === "/api/changes")).toHaveLength(1))
    stream.send("event: hello\ndata: {}\n\n")
    await waitFor(() => expect(result.current).toBe(true))
    expect(invalidated(queryClient)).toEqual(ALL)
  })

  it("opens nothing while the stream is turned off", () => {
    liveConfig.enabled = false
    const { calls } = mockApi({})
    const { wrapper } = withQuery()
    renderHook(() => useChangeStream(), { wrapper })
    expect(calls).toHaveLength(0)
  })

  describe("with the app's query client", () => {
    const PATHS = ["/api/status", "/api/config", "/api/metrics", "/api/token", "/api/storage", "/api/activity", "/api/history", "/api/events"]
    const each = (n: number) => PATHS.map(() => n)
    const HELLO = "event: hello\ndata: {}\n\n"

    function useStreamBacked() {
      useChangeStream()
      useStatus()
      useConfig()
      useMetrics()
      useToken()
      useStorage(undefined)
      useActivity("1h")
      useHistory("", "", "2026-10-01T00:00:00Z")
      useEvents("e1")
    }

    function draw(changes: () => Response) {
      const { calls } = mockApi(authedRoutes({ "GET /api/token": fixtures.token, "GET /api/history": [], "GET /api/changes": changes }))
      const queryClient = trackQueryClient(createQueryClient())
      renderHook(() => useStreamBacked(), {
        wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>,
      })
      return () => PATHS.map((path) => calls.filter((c) => c.path === path).length)
    }

    const flush = () => act(() => vi.advanceTimersByTimeAsync(0))

    beforeEach(() => {
      vi.useFakeTimers()
    })

    afterEach(() => {
      vi.useRealTimers()
    })

    it("fetches each stream-backed query once when a hidden tab is shown again", async () => {
      const streams = [controlled(), controlled()]
      let n = 0
      const fetched = draw(() => streams[n++]?.response ?? json({ error: "not ready" }, 503))
      await flush()
      streams[0]?.send(HELLO)
      await flush()
      expect(fetched()).toEqual(each(1))
      act(() => setVisibility("hidden"))
      act(() => setVisibility("visible"))
      await flush()
      streams[1]?.send(HELLO)
      await flush()
      expect(fetched()).toEqual(each(2))
    })

    it("fetches each stream-backed query once when a tab that loaded hidden is shown", async () => {
      visibility = "hidden"
      const stream = controlled()
      const fetched = draw(() => stream.response)
      await flush()
      expect(fetched()).toEqual(each(1))
      act(() => setVisibility("visible"))
      await flush()
      stream.send(HELLO)
      await flush()
      expect(fetched()).toEqual(each(2))
    })

    it("lets the polls carry a re-shown tab where no stream runs", async () => {
      liveConfig.enabled = false
      const fetched = draw(() => json({ error: "not ready" }, 503))
      await flush()
      act(() => setVisibility("hidden"))
      act(() => setVisibility("visible"))
      await flush()
      expect(fetched()).toEqual(each(1))
      await act(() => vi.advanceTimersByTimeAsync(POLL_SLOW))
      expect(Math.min(...fetched())).toBeGreaterThanOrEqual(2)
    })
  })
})
