import { describe, expect, it } from "vitest"
import { changedKeys, rejected, sameValue, settle, unsavedText, type Equal } from "./draft"

describe("sameValue", () => {
  it("compares lists by element and everything else by identity", () => {
    expect(sameValue(["a", "b"], ["a", "b"])).toBe(true)
    expect(sameValue(["a", "b"], ["b", "a"])).toBe(false)
    expect(sameValue(3, 3)).toBe(true)
    expect(sameValue("3", 3)).toBe(false)
  })
})

describe("changedKeys", () => {
  it("lists the edits that differ from the loaded values", () => {
    const loaded = { mode: "queue", global_max: 2, labels: ["homelab"] }
    expect(changedKeys({ mode: "queue", global_max: 3, labels: ["homelab"] }, loaded)).toEqual(["global_max"])
  })

  it("uses the given comparison", () => {
    const eq: Equal = (key, a, b) => (key === "poll_interval" ? String(a).trim() === String(b).trim() : a === b)
    expect(changedKeys({ poll_interval: " 10s " }, { poll_interval: "10s" }, eq)).toEqual([])
  })
})

describe("settle", () => {
  it("drops edits that were saved and keeps ones made during the save", () => {
    expect(settle({ a: 1, b: 2, c: 3 }, { a: 1, b: 5 })).toEqual({ b: 2, c: 3 })
  })
})

describe("rejected", () => {
  it("shows three messages and counts the rest", () => {
    expect(rejected("a; b")).toEqual({ lines: ["a", "b"], more: 0 })
    expect(rejected("a; b; c; d; e")).toEqual({ lines: ["a", "b", "c"], more: 2 })
  })
})

describe("unsavedText", () => {
  it("counts the changes", () => {
    expect(unsavedText(1)).toBe("1 unsaved change")
    expect(unsavedText(4)).toBe("4 unsaved changes")
  })
})
