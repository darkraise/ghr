import { renderHook, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { appendEvents, appendLog, browserZone, MAX_EVENTS, MAX_LOG, useActivity, useEvents, useHistory, useLogTail } from "./hooks"
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
    expect(appendLog({ text: "a", next: "", drained: false }, { data: "b", next: "c1" })).toEqual({ text: "ab", next: "c1", drained: false })
    expect(appendLog({ text: "x".repeat(MAX_LOG), next: "c1", drained: false }, { data: "yz", next: "c2" }).text).toHaveLength(MAX_LOG)
  })

  const rows = (c: string, n: number) => `${c.repeat(99)}\n`.repeat(n)

  it("is drained once a read comes back empty", () => {
    expect(appendLog({ text: "a", next: "c1", drained: false }, { data: "", next: "c1" }).drained).toBe(true)
    expect(appendLog({ text: "a", next: "c1", drained: true }, { data: "b", next: "c2" }).drained).toBe(false)
  })

  it("cuts an over-long text at a line and puts the header that governs the cut in front", () => {
    const prev = { text: `==> Runner_1.log <==\n${rows("r", 100)}\n==> Worker_2.log <==\n${rows("w", 2800)}`, next: "c1", drained: false }
    const out = appendLog(prev, { data: rows("x", 10), next: "c2" }).text
    const lines = out.split("\n")
    expect(lines[0]).toBe("==> Worker_2.log <==")
    expect(lines.slice(1, -1).every((l) => l === "w".repeat(99) || l === "x".repeat(99))).toBe(true)
    expect(out.endsWith(rows("x", 10))).toBe(true)
    expect(out.length).toBeLessThanOrEqual(MAX_LOG + 21)
  })

  it("does not repeat a header the kept part opens with", () => {
    const front = `==> Runner_1.log <==\n${rows("r", 50)}\n`
    const kept = `==> Worker_2.log <==\n${"w".repeat(MAX_LOG - 23)}\n`
    const out = appendLog({ text: front, next: "c1", drained: false }, { data: kept, next: "c2" }).text
    expect(out).toBe(kept)
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

  it("reads a finished runner's log on until a read comes back empty", { timeout: 10_000 }, async () => {
    const chunks: Record<string, LogChunk> = {
      "": { data: "one\n", next: "c1" },
      c1: { data: "two\n", next: "c2" },
      c2: { data: "", next: "c2" },
    }
    const { calls } = mockApi({
      "GET /api/runners/aaaaaa/log": ({ url }: { url: URL }) => chunks[url.searchParams.get("cursor") ?? ""],
    })
    const { wrapper } = withQuery()
    const { result } = renderHook(() => useLogTail("aaaaaa", true, false), { wrapper })
    await waitFor(() => expect(result.current.data?.drained).toBe(true), { timeout: 4000 })
    await new Promise((r) => setTimeout(r, 1200))
    expect(result.current.data?.text).toBe("one\ntwo\n")
    expect(calls.map((c) => c.search)).toEqual(["?cursor=", "?cursor=c1", "?cursor=c2"])
  })

  it("does not poll a finished runner's log it cannot read", { timeout: 10_000 }, async () => {
    const { calls } = mockApi({})
    const { wrapper } = withQuery()
    renderHook(() => useLogTail("aaaaaa", true, false), { wrapper })
    await new Promise((r) => setTimeout(r, 2500))
    expect(calls).toHaveLength(1)
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

  it("asks for one repository when given", async () => {
    const { calls } = mockApi({ "GET /api/activity": fixtures.activityBuckets })
    const { wrapper } = withQuery()
    const { result } = renderHook(() => useActivity("24h", "darkmem"), { wrapper })
    await waitFor(() => expect(result.current.data?.window).toBe("24h"))
    expect(calls[0]?.search).toBe("?window=24h&tz=UTC&repo=darkmem")
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

describe("useHistory", () => {
  it("waits for since, then asks for up to 500 jobs since it", async () => {
    const { calls } = mockApi({ "GET /api/history": fixtures.history })
    const { wrapper } = withQuery()
    const { result, rerender } = renderHook(({ since }: { since: string | undefined }) => useHistory("darkmem", "", since), {
      wrapper,
      initialProps: { since: undefined as string | undefined },
    })
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(calls).toHaveLength(0)
    rerender({ since: "2026-10-03T00:00:00Z" })
    await waitFor(() => expect(result.current.data).toHaveLength(2))
    expect(calls[0]?.search).toBe("?repo=darkmem&conclusion=&since=2026-10-03T00%3A00%3A00Z&limit=500")
  })
})
