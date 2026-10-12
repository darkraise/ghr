import { describe, expect, it } from "vitest"
import { resultState, stateTone, stateWord } from "./status"

describe("resultState", () => {
  it.each([
    ["success", "Succeeded", "ok"],
    ["cancelled", "Cancelled", "muted"],
    ["skipped", "Skipped", "muted"],
    ["unknown", "Unknown", "muted"],
    ["neutral", "Neutral", "muted"],
    ["stale", "Stale", "muted"],
    ["action_required", "Action required", "warn"],
    ["failure", "Failed", "bad"],
    ["timed_out", "Failed", "bad"],
    ["startup_failure", "Failed", "bad"],
    ["a_result_added_later", "Failed", "bad"],
    ["", "Failed", "bad"],
  ])("reads %s as %s in the %s tone", (conclusion, word, tone) => {
    const state = resultState(conclusion)
    expect(stateWord(state)).toBe(word)
    expect(stateTone(state)).toBe(tone)
  })
})
