import { act, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { InstanceStatus, MetricSample } from "@/api/types"
import { ago, clock, dateTime, dateTimeSec, dur, elapsed, fmtMem, hhmm, humanBytes, isZeroTime, maxText, series } from "./format"
import { useNow } from "./use-now"

afterEach(() => vi.useRealTimers())

describe("dur", () => {
  it("prints minutes and seconds under an hour", () => {
    expect(dur(65_000)).toBe("1m05s")
    expect(dur(0)).toBe("0m00s")
    expect(dur(-5_000)).toBe("0m00s")
  })
  it("prints hours and minutes from an hour", () => expect(dur(3_725_000)).toBe("1h02m"))
  it("rounds to the second", () => expect(dur(59_600)).toBe("1m00s"))
})

describe("ago", () => {
  it("matches the TUI's buckets", () => {
    expect(ago(30_000)).toBe("just now")
    expect(ago(5 * 60_000)).toBe("5m ago")
    expect(ago(3 * 3_600_000)).toBe("3h ago")
    expect(ago(47 * 3_600_000)).toBe("47h ago")
    expect(ago(48 * 3_600_000)).toBe("2d ago")
  })
})

describe("humanBytes", () => {
  it("uses decimal units with one decimal", () => {
    expect(humanBytes(0)).toBe("0 B")
    expect(humanBytes(999)).toBe("999 B")
    expect(humanBytes(1000)).toBe("1.0 kB")
    expect(humanBytes(1_234_567)).toBe("1.2 MB")
    expect(humanBytes(6_571_000_000)).toBe("6.6 GB")
  })
  it("rounds before picking the unit", () => {
    expect(humanBytes(999_949)).toBe("999.9 kB")
    expect(humanBytes(999_950)).toBe("1.0 MB")
    expect(humanBytes(999_950_000)).toBe("1.0 GB")
  })
})

describe("small formatters", () => {
  it("fmtMem prints binary G or M", () => {
    expect(fmtMem(3 * 2 ** 30)).toBe("3.0G")
    expect(fmtMem(512 * 2 ** 20)).toBe("512M")
  })
  it("maxText shows 0 as unlimited", () => {
    expect(maxText(0)).toBe("∞")
    expect(maxText(3)).toBe("3")
  })
  it("isZeroTime spots Go's zero time", () => {
    expect(isZeroTime("0001-01-01T00:00:00Z")).toBe(true)
    expect(isZeroTime(undefined)).toBe(true)
    expect(isZeroTime("2026-10-03T14:05:00Z")).toBe(false)
  })
  it("formats local times (TZ=UTC in tests)", () => {
    expect(clock("2026-10-03T14:05:09Z")).toBe("14:05:09")
    expect(hhmm(new Date("2026-10-06T14:20:00Z"))).toBe("14:20")
    expect(dateTime("2026-10-03T14:05:09Z")).toBe("2026-10-03 14:05")
    expect(dateTimeSec("2026-10-03T14:05:09Z")).toBe("2026-10-03 14:05:09")
  })
})

describe("elapsed", () => {
  const now = Date.parse("2026-10-03T14:05:00Z")
  const base: InstanceStatus = { id: "a", repo: "r", runner_name: "n", state: "busy", since: "2026-10-03T14:00:00Z" }
  it("counts from the job's start when there is one", () => {
    const job = { run_id: 1, run_number: "1", workflow: "w", name: "j", started_at: "2026-10-03T14:04:00Z" }
    expect(elapsed({ ...base, job }, now)).toBe("1m00s")
  })
  it("counts from the state change when the job has not started", () => {
    const job = { run_id: 1, run_number: "1", workflow: "w", name: "j", started_at: "0001-01-01T00:00:00Z" }
    expect(elapsed({ ...base, job }, now)).toBe("5m00s")
    expect(elapsed(base, now)).toBe("5m00s")
  })
})

describe("series", () => {
  it("leaves a gap where samples are more than 90 s apart", () => {
    const s = (at: string, live: number): MetricSample => ({ at, live, queued: 0 })
    const samples = [s("2026-10-03T14:00:00Z", 1), s("2026-10-03T14:01:00Z", 2), s("2026-10-03T14:05:00Z", 3)]
    expect(series(samples, (x) => x.live)).toEqual([1, 2, null, 3])
    expect(series(samples, (x) => x.cpu)).toEqual([null, null, null, null])
  })
})

describe("useNow", () => {
  it("ticks every second", () => {
    vi.useFakeTimers({ now: new Date("2026-10-03T14:05:00Z") })
    const { result } = renderHook(() => useNow())
    expect(result.current).toBe(Date.parse("2026-10-03T14:05:00Z"))
    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(result.current).toBe(Date.parse("2026-10-03T14:05:01Z"))
  })
})
