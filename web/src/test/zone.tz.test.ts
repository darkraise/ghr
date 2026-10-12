import { describe, expect, it } from "vitest"

describe("the time-zone suite", () => {
  it("runs in Europe/Berlin, a zone with daylight saving", () => {
    expect(Intl.DateTimeFormat().resolvedOptions().timeZone).toBe("Europe/Berlin")
    expect(new Date("2026-01-15T12:00:00Z").getHours()).toBe(13)
    expect(new Date("2026-07-15T12:00:00Z").getHours()).toBe(14)
  })
})
