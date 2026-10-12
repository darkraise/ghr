import type { QueryClient } from "@tanstack/react-query"
import { act, renderHook, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { json, mockApi } from "@/test/api"
import { withQuery } from "@/test/query"
import { keys } from "./hooks"
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

function setVisibility(v: DocumentVisibilityState) {
  visibility = v
  document.dispatchEvent(new Event("visibilitychange"))
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
})
