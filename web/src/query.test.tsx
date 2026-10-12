import { focusManager, onlineManager, QueryClientProvider } from "@tanstack/react-query"
import { act, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { useActions, useActivity, useConfig, useEvents, useHistory, useMetrics, useStatus, useStorage, useToken } from "@/api/hooks"
import { mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { trackQueryClient } from "@/test/query-clients"
import { createQueryClient } from "./query"

function withAppClient() {
  const queryClient = trackQueryClient(createQueryClient())
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  return { queryClient, wrapper }
}

const routes = () => authedRoutes({ "GET /api/token": fixtures.token, "GET /api/history": [] })
const flush = () => act(() => vi.advanceTimersByTimeAsync(0))

describe("createQueryClient", () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    focusManager.setFocused(undefined)
    onlineManager.setOnline(true)
    vi.useRealTimers()
  })

  // Shows the tab again, then brings the network back, and counts the
  // requests for path after each.
  async function refetches(path: string, use: () => unknown): Promise<number[]> {
    const { calls } = mockApi(routes())
    const { wrapper } = withAppClient()
    renderHook(use, { wrapper })
    await flush()
    const fetched = () => calls.filter((c) => c.path === path).length
    const counts = [fetched()]
    focusManager.setFocused(false)
    focusManager.setFocused(true)
    await flush()
    counts.push(fetched())
    onlineManager.setOnline(false)
    onlineManager.setOnline(true)
    await flush()
    counts.push(fetched())
    return counts
  }

  it.each<[string, string, () => unknown]>([
    ["useStatus", "/api/status", () => useStatus()],
    ["useConfig", "/api/config", () => useConfig()],
    ["useMetrics", "/api/metrics", () => useMetrics()],
    ["useToken", "/api/token", () => useToken()],
    ["useStorage", "/api/storage", () => useStorage(undefined)],
    ["useActivity", "/api/activity", () => useActivity("1h")],
    ["useHistory", "/api/history", () => useHistory("", "", "2026-10-01T00:00:00Z")],
    ["useEvents", "/api/events", () => useEvents("e1")],
  ])("leaves %s to the change stream on a re-shown tab and a network return", async (_name, path, use) => {
    expect(await refetches(path, use)).toEqual([1, 1, 1])
  })

  it("still refetches a query the stream does not cover", async () => {
    expect(await refetches("/api/actions", () => useActions())).toEqual([1, 2, 3])
  })
})
