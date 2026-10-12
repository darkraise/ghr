import { describe, expect, it } from "vitest"
import { pickText } from "./history"

// The daemon aligns six-hour and day buckets to local wall time, so in
// Europe/Berlin they change length on 2026-03-29 and 2026-10-25.
describe("pickText across a daylight-saving change", () => {
  it.each([
    ["a 23-hour day", "2026-03-29T00:00:00+01:00", "2026-03-30T00:00:00+02:00", "Showing Mar 29"],
    ["a 25-hour day", "2026-10-25T00:00:00+02:00", "2026-10-26T00:00:00+01:00", "Showing Oct 25"],
    ["a 5-hour six-hour bucket", "2026-03-29T00:00:00+01:00", "2026-03-29T06:00:00+02:00", "Showing 00:00 to 06:00, Mar 29"],
    ["a 7-hour six-hour bucket", "2026-10-25T00:00:00+02:00", "2026-10-25T06:00:00+01:00", "Showing 00:00 to 06:00, Oct 25"],
  ])("reads %s", (_name, start, end, text) => {
    expect(pickText({ start, end })).toBe(text)
  })
})
