import { describe, expect, it } from "vitest"
import { classify, matchLabels } from "./labels"

const effective = ["self-hosted", "linux", "x64", "homelab", "gpu"]

describe("matchLabels", () => {
  it("needs every job label among the effective ones, ignoring case", () => {
    expect(matchLabels(["Self-Hosted", "GPU"], effective)).toBe(true)
    expect(matchLabels(["self-hosted", "big"], effective)).toBe(false)
  })
})

describe("classify", () => {
  it("matches what ghr takes", () => {
    expect(classify(["self-hosted", "homelab"], effective)).toEqual({ kind: "matched", missing: [] })
  })

  it("lists what a self-hosted group misses", () => {
    expect(classify(["self-hosted", "arm64", "big"], effective)).toEqual({ kind: "unmatched", missing: ["arm64", "big"] })
  })

  it("leaves runner groups and GitHub-hosted jobs alone", () => {
    expect(classify([], effective)).toEqual({ kind: "other", missing: [] })
    expect(classify(["ubuntu-latest"], effective)).toEqual({ kind: "other", missing: [] })
  })
})
