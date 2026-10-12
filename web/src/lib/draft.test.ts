import { describe, expect, it } from "vitest"
import { changedKeys, prune, rejected, sameValue, settle, unsavedText, type Equal } from "./draft"

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

describe("prune", () => {
  it("drops the entries that hold the loaded value and keeps the rest", () => {
    const loaded = { mode: "queue", global_max: 2, labels: ["homelab"], max: undefined }
    expect(prune({ mode: "queue", global_max: 3, labels: ["homelab"], max: undefined }, loaded)).toEqual({ global_max: 3 })
  })

  it("compares the stored values, not what they mean", () => {
    const draft = { start_timeout: "60s", memory_max: "6G " }
    expect(prune(draft, { start_timeout: "1m", memory_max: "6G" })).toEqual(draft)
  })

  it("returns the draft itself when nothing is dropped", () => {
    const draft = { global_max: 3 }
    expect(prune(draft, { global_max: 2 })).toBe(draft)
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
