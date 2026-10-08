import { renderHook, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { appendEvents, appendLog, browserZone, MAX_EVENTS, MAX_LOG, useActivity, useEvents, useLogTail } from "./hooks"
import type { ActivityWindow, GhrEvent, LogChunk } from "./types"

const ev = (seq: number): GhrEvent => ({ seq, time: "2026-10-03T14:05:00Z", level: "info", msg: `event ${seq}` })

describe("appendEvents", () => {
  it("appends only events newer than the last one held", () => {
    expect(appendEvents([ev(1), ev(2)], [ev(2), ev(3)]).map((e) => e.seq)).toEqual([1, 2, 3])
  })

  it("keeps the last 200", () => {
    const prev = Array.from({ length: MAX_EVENTS }, (_, i) => ev(i + 1))
    const out = appendEvents(prev, [ev(201), ev(202)])
    expect(out).toHaveLength(MAX_EVENTS)
    expect(out[0]?.seq).toBe(3)
    expect(out.at(-1)?.seq).toBe(202)
  })
})

describe("appendLog", () => {
  it("appends the data, advances the cursor and caps the text", () => {
    expect(appendLog({ text: "a", next: "" }, { data: "b", next: "c1" })).toEqual({ text: "ab", next: "c1" })
    expect(appendLog({ text: "x".repeat(MAX_LOG), next: "c1" }, { data: "yz", next: "c2" }).text).toHaveLength(MAX_LOG)
  })
})

describe("useEvents", () => {
  it("polls by cursor and starts over when the epoch changes", { timeout: 10_000 }, async () => {
    let restarted = false
    const { calls } = mockApi({
      "GET /api/events": ({ url }: { url: URL }) => {
        const after = Number(url.searchParams.get("after"))
        if (restarted) return after === 0 ? [ev(1)] : []
        if (after === 0) return [ev(1), ev(2)]
        return after === 2 ? [ev(3)] : []
      },
    })
    const { wrapper } = withQuery()
    const { result, rerender } = renderHook(({ epoch }: { epoch: string }) => useEvents(epoch), {
      wrapper,
      initialProps: { epoch: "a" },
    })
    await waitFor(() => expect(result.current.map((e) => e.seq)).toEqual([1, 2, 3]), { timeout: 4000 })
    restarted = true
    rerender({ epoch: "b" })
    await waitFor(() => expect(result.current.map((e) => e.seq)).toEqual([1]), { timeout: 4000 })
    expect(calls.filter((c) => c.search === "?after=0")).toHaveLength(2)
  })

  it("waits for an epoch before polling", { timeout: 10_000 }, async () => {
    const { calls } = mockApi({ "GET /api/events": [] })
    const { wrapper } = withQuery()
    renderHook(() => useEvents(undefined), { wrapper })
    await new Promise((r) => setTimeout(r, 1200))
    expect(calls).toHaveLength(0)
  })
})

describe("useLogTail", () => {
  it("advances the cursor on every poll", { timeout: 10_000 }, async () => {
    const chunks: Record<string, LogChunk> = {
      "": { data: "one\n", next: "c1" },
      c1: { data: "two\n", next: "c2" },
      c2: { data: "", next: "c2" },
    }
    const { calls } = mockApi({
      "GET /api/runners/aaaaaa/log": ({ url }: { url: URL }) => chunks[url.searchParams.get("cursor") ?? ""],
    })
    const { wrapper } = withQuery()
    const { result } = renderHook(() => useLogTail("aaaaaa", true), { wrapper })
    await waitFor(() => expect(result.current.data?.text).toBe("one\ntwo\n"), { timeout: 4000 })
    await new Promise((r) => setTimeout(r, 1200))
    expect(result.current.data?.text).toBe("one\ntwo\n")
    expect(calls.map((c) => c.search).slice(0, 2)).toEqual(["?cursor=", "?cursor=c1"])
  })

  it("does not poll while the log is closed", { timeout: 10_000 }, async () => {
    const { calls } = mockApi({ "GET /api/runners/aaaaaa/log": { data: "x", next: "c1" } })
    const { wrapper } = withQuery()
    renderHook(() => useLogTail("aaaaaa", false), { wrapper })
    await new Promise((r) => setTimeout(r, 1200))
    expect(calls).toHaveLength(0)
  })
})

describe("useActivity", () => {
  it("asks for the window in the browser's zone", async () => {
    const { calls } = mockApi({ "GET /api/activity": fixtures.activityLanes })
    const { wrapper } = withQuery()
    const { result } = renderHook(() => useActivity("3h"), { wrapper })
    await waitFor(() => expect(result.current.data?.capacity).toBe(2))
    expect(browserZone()).toBe("UTC")
    expect(calls[0]?.search).toBe("?window=3h&tz=UTC")
  })

  it("keeps the last window's data while the next one loads", async () => {
    let release = () => {}
    const gate = new Promise<void>((resolve) => (release = resolve))
    mockApi({
      "GET /api/activity": async ({ url }: { url: URL }) => {
        if (url.searchParams.get("window") !== "24h") return fixtures.activityLanes
        await gate
        return fixtures.activityBuckets
      },
    })
    const { wrapper } = withQuery()
    const { result, rerender } = renderHook(({ w }) => useActivity(w), { wrapper, initialProps: { w: "1h" as ActivityWindow } })
    await waitFor(() => expect(result.current.data?.window).toBe("1h"))
    rerender({ w: "24h" })
    expect(result.current.data?.window).toBe("1h")
    expect(result.current.isPlaceholderData).toBe(true)
    release()
    await waitFor(() => expect(result.current.data?.window).toBe("24h"))
  })
})
