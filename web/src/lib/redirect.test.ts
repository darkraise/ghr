import { describe, expect, it } from "vitest"
import { loginSearch, safeRedirect } from "./redirect"

describe("safeRedirect", () => {
  it.each([
    ["/history", "/history"],
    ["/history?result=failure#top", "/history?result=failure#top"],
    ["/repositories/a%20b%5Cc", "/repositories/a%20b%5Cc"],
    ["/ /evil.example/x", "/%20/evil.example/x"],
    ["/history x", "/history%20x"],
    ["/history\\x", "/history/x"],
    ["/history\u0000", "/history"],
    ["/history\u007f", "/history%7F"],
    ["/history\u0085", "/history%C2%85"],
  ])("keeps the path %j on this site, as the browser parses it", (target, want) => {
    expect(safeRedirect(target)).toBe(want)
  })

  it.each([
    undefined,
    "",
    "history",
    "https://evil.example/x",
    "//evil.example/x",
    "/\\evil.example/x",
    "/\t/evil.example/x",
    "/\n/evil.example/x",
    "/\r/evil.example/x",
  ])("falls back to / for %j", (target) => {
    expect(safeRedirect(target)).toBe("/")
  })
})

describe("loginSearch", () => {
  it("names the page to come back to, except the dashboard", () => {
    expect(loginSearch("/history?result=failure")).toEqual({ redirect: "/history?result=failure" })
    expect(loginSearch("/")).toEqual({})
  })
})
