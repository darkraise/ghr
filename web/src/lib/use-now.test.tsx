import { act, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { keys } from "@/api/hooks"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { useNow } from "./use-now"

const at = (time: string) => Date.parse(`2026-10-03T${time}Z`)

describe("useNow", () => {
  beforeEach(() => {
    vi.useFakeTimers({ now: at("14:05:00") })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it("serves every caller from one interval and stops it with the last", () => {
    const { wrapper } = withQuery()
    const first = renderHook(() => useNow(), { wrapper })
    const second = renderHook(() => useNow(), { wrapper })
    expect(vi.getTimerCount()).toBe(1)
    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect([first.result.current, second.result.current]).toEqual([at("14:05:01"), at("14:05:01")])
    first.unmount()
    expect(vi.getTimerCount()).toBe(1)
    second.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it("renders the current time first after standing idle", () => {
    const { wrapper } = withQuery()
    renderHook(() => useNow(), { wrapper }).unmount()
    vi.setSystemTime(at("14:06:00"))
    const rendered: number[] = []
    renderHook(() => rendered.push(useNow()), { wrapper })
    expect(rendered[0]).toBe(at("14:06:00"))
  })

  it("does not run behind a status that arrived between two ticks", () => {
    const { queryClient, wrapper } = withQuery()
    const { result } = renderHook(() => useNow(), { wrapper })
    act(() => {
      vi.advanceTimersByTime(400)
      queryClient.setQueryData(keys.status, { ...fixtures.status, now: "2026-10-03T14:07:30.400Z" })
    })
    expect(result.current).toBe(at("14:07:30.400"))
  })
})
