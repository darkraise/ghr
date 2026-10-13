import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { StorageAlert } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { bannerText, canDismiss, dismiss, gbh, isDismissed, utcDay } from "./quota"

const alert = (over: Partial<StorageAlert>): StorageAlert => ({ ...fixtures.storageAlert, ...over })

describe("quota text", () => {
  it.each([
    [373.31, "373 GB-h"],
    [10, "10 GB-h"],
    [2.46, "2.5 GB-h"],
    [0, "0 GB-h"],
  ])("writes %d GB-hours as %s", (n, want) => {
    expect(gbh(n)).toBe(want)
  })

  it("writes a day in UTC", () => {
    expect(utcDay("2026-10-24T23:30:00Z")).toBe("Oct 24")
    expect(utcDay("2026-11-01T00:00:00Z")).toBe("Nov 1")
  })

  it.each<[Partial<StorageAlert>, string]>([
    [{ level: "exhausted", used_pct: 103 }, "GitHub artifact storage for October is used up: uploads fail until Nov 1."],
    [{ level: "critical" }, "GitHub artifact storage is at 91% of October's allowance and runs out around Oct 6."],
    [{ level: "warn", used_pct: 72, runs_out_at: undefined }, "GitHub artifact storage is at 72% of October's allowance."],
    [{ level: "projected", used_pct: 40, runs_out_at: "2026-10-24T11:45:00Z" }, "GitHub artifact storage runs out around Oct 24, before the month resets on Nov 1."],
    [{ level: "projected", used_pct: 40, runs_out_at: undefined }, "GitHub artifact storage runs out before the month resets on Nov 1."],
  ])("words the %j banner", (over, want) => {
    expect(bannerText(alert(over))).toBe(want)
  })

  it("lets only the early levels be dismissed", () => {
    expect(["", "projected", "warn", "critical", "exhausted"].map(canDismiss)).toEqual([false, true, true, false, false])
  })
})

describe("banner dismissal", () => {
  beforeEach(() => localStorage.clear())
  afterEach(() => vi.restoreAllMocks())

  it("keeps a dismissal per level and month", () => {
    const warn = alert({ level: "warn" })
    expect(isDismissed(warn)).toBe(false)
    dismiss(warn)
    expect(isDismissed(warn)).toBe(true)
    expect(isDismissed(alert({ level: "projected" }))).toBe(false)
    expect(isDismissed(alert({ level: "warn", month: "2026-11" }))).toBe(false)
  })

  it("never hides a level that cannot be dismissed", () => {
    const critical = alert({ level: "critical" })
    dismiss(critical)
    expect(isDismissed(critical)).toBe(false)
  })

  it("shows the banner when local storage cannot be read or written", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied")
    })
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("denied")
    })
    const warn = alert({ level: "warn" })
    expect(() => dismiss(warn)).not.toThrow()
    expect(isDismissed(warn)).toBe(false)
  })
})
