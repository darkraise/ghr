import { describe, expect, it } from "vitest"
import { UNIT_SETTINGS, readSetting, writeSetting } from "./units"

describe("readSetting", () => {
  it.each([
    ["poll_interval", "10s", 10],
    ["start_timeout", "2m0s", 120],
    ["idle_timeout", "1.5s", 1.5],
    ["history_retention", "30d", 30],
    ["history_retention", "36h0m0s", 1.5],
    ["build_cache_keep", "20GB", 20],
    ["build_cache_keep", "2048MB", 2],
    ["build_cache_keep", "1TB", 1024],
    ["build_cache_keep", "0B", 0],
    ["memory_max", "6G", 6144],
    ["memory_max", "512M", 512],
    ["memory_max", "1073741824", 1024],
    ["cpu_quota", "200%", 200],
  ])("reads %s %s as %d", (key, raw, n) => {
    expect(readSetting(key, raw)).toBe(n)
  })

  it("reads an unlimited memory_max as an empty field", () => {
    expect(readSetting("memory_max", "infinity")).toBeNaN()
  })

  it.each([
    ["memory_max", "50%"],
    ["memory_max", "100K"],
    ["build_cache_keep", "500MB"],
  ])("keeps %s %s as text the unit cannot show", (key, raw) => {
    expect(readSetting(key, raw)).toBe(raw)
  })
})

describe("writeSetting", () => {
  it.each([
    ["poll_interval", 15, "15s"],
    ["history_retention", 30, "30d"],
    ["history_retention", 1.5, "129600s"],
    ["build_cache_keep", 20, "20GB"],
    ["memory_max", 8192, "8192M"],
    ["memory_max", Number.NaN, "infinity"],
    ["cpu_quota", 150, "150%"],
  ])("writes %s %d as %s", (key, n, raw) => {
    expect(writeSetting(key, n)).toBe(raw)
  })

  it("sends kept text and other settings as they are", () => {
    expect(writeSetting("memory_max", "50%")).toBe("50%")
    expect(writeSetting("global_max", 3)).toBe(3)
  })
})

describe("unit errors", () => {
  const error = (key: string, n: number) => UNIT_SETTINGS[key]?.error(n)

  it("holds each setting to the daemon's floor", () => {
    expect(error("poll_interval", 4)).toBe("Enter at least 5 seconds")
    expect(error("poll_interval", 5)).toBe("")
    expect(error("start_timeout", 0)).toBe("Enter more than 0 seconds")
    expect(error("idle_timeout", Number.NaN)).toBe("Enter a number of seconds")
    expect(error("history_retention", 0.5)).toBe("Enter at least 1 day")
    expect(error("build_cache_keep", 0)).toBe("")
    expect(error("build_cache_keep", 1.5)).toBe("Enter a whole number of 0 or more")
    expect(error("cpu_quota", 0)).toBe("Enter a whole number of 1 or more")
  })

  it("lets memory_max be empty but not fractional", () => {
    expect(error("memory_max", Number.NaN)).toBe("")
    expect(error("memory_max", 1.5)).toBe("Enter a whole number of MB, or leave it empty for no limit")
  })
})
