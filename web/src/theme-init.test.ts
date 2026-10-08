import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { beforeEach, describe, expect, it } from "vitest"

const script = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "../public/theme-init.js"), "utf8")
const run = () => new Function(script)()
const root = document.documentElement

describe("theme-init.js", () => {
  beforeEach(() => {
    localStorage.clear()
    for (const name of root.getAttributeNames()) if (name.startsWith("data-")) root.removeAttribute(name)
  })

  it("clears the old theme settings once and keeps the mode", () => {
    localStorage.setItem("theme-preset", "glass")
    localStorage.setItem("theme-density", "spacious")
    localStorage.setItem("mode", "light")
    localStorage.setItem("ghr-activity-window", "24h")
    run()
    expect(localStorage.getItem("theme-preset")).toBeNull()
    expect(localStorage.getItem("theme-density")).toBeNull()
    expect(localStorage.getItem("mode")).toBe("light")
    expect(localStorage.getItem("ghr-activity-window")).toBe("24h")
    expect(localStorage.getItem("ghr-theme-v2")).toBe("1")
    expect(root.getAttribute("data-mode")).toBe("light")
  })

  it("clears them only the first time", () => {
    localStorage.setItem("ghr-theme-v2", "1")
    localStorage.setItem("theme-radius", "pill")
    run()
    expect(localStorage.getItem("theme-radius")).toBe("pill")
  })

  it("falls back to dark", () => {
    run()
    expect(root.getAttribute("data-mode")).toBe("dark")
  })

  it("resolves system from the media query", () => {
    localStorage.setItem("mode", "system")
    run()
    expect(root.getAttribute("data-mode")).toBe("light")
  })

  it("pins the theme axes before React mounts", () => {
    run()
    expect(root.getAttribute("data-density")).toBe("compact")
    expect(root.getAttribute("data-radius")).toBe("subtle")
    expect(root.getAttribute("data-font-size")).toBe("medium")
  })
})
