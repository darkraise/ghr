import { describe, expect, it } from "vitest"
import { DAY_MS, durationError, parseDuration, sameDuration } from "./duration"

describe("parseDuration", () => {
  it.each([
    ["10s", 10_000],
    ["2m0s", 120_000],
    ["1h30m", 5_400_000],
    ["1.5h", 5_400_000],
    ["250ms", 250],
    ["30d", 30 * DAY_MS],
    ["0", 0],
    ["-5s", -5000],
  ])("reads %s", (text, ms) => {
    expect(parseDuration(text)).toBe(ms)
  })

  it.each(["", "5", "abc", "1d12h", "1.5d", "5 s", "s"])("rejects %j", (text) => {
    expect(parseDuration(text)).toBeNull()
  })
})

describe("durationError", () => {
  it("passes a valid duration", () => {
    expect(durationError("poll_interval", " 15s ", 5000)).toBe("")
  })

  it("names what is wrong", () => {
    expect(durationError("poll_interval", "soon", 5000)).toBe('invalid duration "soon"')
    expect(durationError("start_timeout", "0s", 0)).toBe("start_timeout must be greater than 0")
    expect(durationError("poll_interval", "2s", 5000)).toBe("poll_interval must be at least 5s")
    expect(durationError("history_retention", "12h", DAY_MS)).toBe("history_retention must be at least 1d")
  })
})

describe("sameDuration", () => {
  it("compares by value, falling back to the text", () => {
    expect(sameDuration("120s", "2m0s")).toBe(true)
    expect(sameDuration("30d", "720h")).toBe(true)
    expect(sameDuration("10s", "11s")).toBe(false)
    expect(sameDuration("bogus", "bogus")).toBe(true)
  })
})
