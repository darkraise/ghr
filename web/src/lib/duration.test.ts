import { describe, expect, it } from "vitest"
import { DAY_MS, parseDuration } from "./duration"

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
