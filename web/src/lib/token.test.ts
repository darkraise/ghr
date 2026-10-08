import { describe, expect, it } from "vitest"
import type { TokenStatus } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { expiryText, rateText, tokenWord } from "./token"

const DAY = 86_400_000
const now = Date.parse("2026-10-03T14:05:00Z")
const at = (ms: number) => new Date(now + ms).toISOString()
const token = (over: Partial<TokenStatus>): TokenStatus => ({ state: "ok", ...over })

describe("tokenWord", () => {
  it.each([
    [token({ expires_at: at(60 * DAY) }), "valid"],
    [token({}), "valid"],
    [token({ expires_at: at(10 * DAY) }), "expires soon"],
    [token({ state: "unverified" }), "unverified"],
    [token({ state: "rejected", expires_at: at(DAY) }), "rejected"],
  ])("reads %j as %s", (t, word) => {
    expect(tokenWord(t, now)).toBe(word)
  })
})

describe("expiryText", () => {
  it.each([
    [token({ expires_at: at(60 * DAY + 3_600_000) }), "Expires Dec 2, in 60 days"],
    [token({ expires_at: at(DAY + 3_600_000) }), "Expires Oct 4, in 1 day"],
    [token({ expires_at: at(2 * 3_600_000) }), "Expires Oct 3, in less than a day"],
    [token({ expires_at: at(-DAY) }), "Expired Oct 2"],
    [token({}), "Expiry unknown"],
  ])("reads %j as %s", (t, text) => {
    expect(expiryText(t, now)).toBe(text)
  })
})

describe("rateText", () => {
  it("shows what is left of the limit and when it resets", () => {
    expect(rateText(fixtures.token)).toBe("4,980 of 5,000, resets 14:45")
    expect(rateText({ ...fixtures.token, rate_limit: undefined })).toBe("4,980, resets 14:45")
    expect(rateText({ ...fixtures.token, rate_reset: undefined })).toBe("4,980 of 5,000")
  })
  it("is blank until known", () => {
    expect(rateText(undefined)).toBe("")
    expect(rateText(token({}))).toBe("")
  })
})
