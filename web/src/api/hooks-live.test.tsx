import type { Query, QueryClient, QueryKey } from "@tanstack/react-query"
import { act, renderHook, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import {
  browserZone,
  keys,
  livePoll,
  POLL_ACTIONS,
  POLL_FAST,
  POLL_SAFETY,
  POLL_SLOW,
  useActions,
  useActivity,
  useConfig,
  useContainers,
  useEvents,
  useGitHubStorage,
  useHistory,
  useLogTail,
  useMetrics,
  useStatus,
  useSteps,
  useStorage,
  useToken,
} from "./hooks"
import { setLive } from "./live"

type Interval = number | false | undefined

// interval resolves the refetch interval the query's mounted observer asks for now.
function interval(qc: QueryClient, key: QueryKey): Interval {
  const q = qc.getQueryCache().find({ queryKey: key, exact: true })
  if (!q) return undefined
  const opt = q.observers[0]?.options.refetchInterval as Interval | ((q: Query) => Interval)
  return typeof opt === "function" ? opt(q) : opt
}

// The storage fixture has an install running; these cases need it idle.
const idleStorage = { ...fixtures.storage, measuring: false, operations: { current: null, queued: 0, recent: [] } }
const routes = () => authedRoutes({ "GET /api/token": fixtures.token, "GET /api/history": [], "GET /api/storage": idleStorage })

interface Case {
  name: string
  key: QueryKey
  want: number
  use: () => unknown
}

afterEach(() => setLive(false))

const SINCE = "2026-10-01T00:00:00Z"

describe("daemon-state hooks", () => {
  // want is the interval while the stream is not live.
  const cases: Case[] = [
    { name: "useStatus", key: keys.status, want: POLL_FAST, use: () => useStatus() },
    { name: "useEvents", key: keys.events("e1"), want: POLL_FAST, use: () => useEvents("e1") },
    { name: "useConfig", key: keys.config, want: POLL_SLOW, use: () => useConfig() },
    { name: "useMetrics", key: keys.metrics, want: POLL_SLOW, use: () => useMetrics() },
    { name: "useToken", key: keys.token, want: POLL_SLOW, use: () => useToken() },
    { name: "useActivity", key: keys.activity("1h", browserZone(), ""), want: POLL_SLOW, use: () => useActivity("1h") },
    { name: "useHistory", key: keys.history("", "", SINCE), want: POLL_SLOW, use: () => useHistory("", "", SINCE) },
    { name: "useStorage", key: keys.storage, want: POLL_SLOW, use: () => useStorage(undefined) },
    { name: "useGitHubStorage", key: keys.githubStorage, want: POLL_SLOW, use: () => useGitHubStorage() },
  ]

  it.each(cases)("$name polls every 60 s while live", async ({ key, want, use }) => {
    mockApi(routes())
    const { queryClient, wrapper } = withQuery()
    renderHook(use, { wrapper })
    await waitFor(() => expect(interval(queryClient, key)).toBe(want))
    act(() => setLive(true))
    await waitFor(() => expect(interval(queryClient, key)).toBe(POLL_SAFETY))
    act(() => setLive(false))
    await waitFor(() => expect(interval(queryClient, key)).toBe(want))
  })

  it("gives up the busy storage poll while live", async () => {
    // The fixture's running install makes storage busy.
    mockApi(authedRoutes())
    const { queryClient, wrapper } = withQuery()
    renderHook(() => useStorage(undefined), { wrapper })
    await waitFor(() => expect(interval(queryClient, keys.storage)).toBe(POLL_FAST))
    act(() => setLive(true))
    await waitFor(() => expect(interval(queryClient, keys.storage)).toBe(POLL_SAFETY))
  })
})

describe("useGitHubStorage", () => {
  it("polls every second while a bulk operation runs, and every 60 s while live", async () => {
    const running = { id: "op-2", kind: "delete-caches", repo: "darkmem", total: 4, done: 1, bytes: 0, failed: [], started_at: "2026-10-03T14:04:00Z" }
    mockApi(authedRoutes({ "GET /api/github-storage": { ...fixtures.githubStorage, operation: running } }))
    const { queryClient, wrapper } = withQuery()
    renderHook(() => useGitHubStorage(), { wrapper })
    await waitFor(() => expect(interval(queryClient, keys.githubStorage)).toBe(POLL_FAST))
    act(() => setLive(true))
    await waitFor(() => expect(interval(queryClient, keys.githubStorage)).toBe(POLL_SAFETY))
  })
})

describe("hooks the stream does not cover", () => {
  const cases: Case[] = [
    { name: "useActions", key: keys.actions, want: POLL_ACTIONS, use: () => useActions() },
    { name: "useSteps", key: keys.steps("aaaaaa"), want: POLL_SLOW, use: () => useSteps("aaaaaa", true) },
    { name: "useContainers", key: keys.containers("aaaaaa"), want: POLL_SLOW, use: () => useContainers("aaaaaa", true) },
    { name: "useLogTail", key: keys.log("aaaaaa"), want: POLL_FAST, use: () => useLogTail("aaaaaa", true) },
  ]

  it.each(cases)("$name keeps its interval while live", async ({ key, want, use }) => {
    mockApi(routes())
    const { queryClient, wrapper } = withQuery()
    act(() => setLive(true))
    renderHook(use, { wrapper })
    await waitFor(() => expect(interval(queryClient, key)).toBe(want))
  })
})

describe("livePoll", () => {
  it("swaps the interval for the safety poll while live", () => {
    expect(livePoll(true, POLL_FAST)).toBe(POLL_SAFETY)
    expect(livePoll(false, POLL_FAST)).toBe(POLL_FAST)
  })
})
