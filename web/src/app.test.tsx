import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { App } from "./app"

describe("App", () => {
  it("renders the ghr heading", () => {
    render(<App />)
    expect(screen.getByRole("heading", { name: "ghr" })).toBeInTheDocument()
  })
})

describe("index.html", () => {
  const html = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "../index.html"), "utf8")

  it("has no inline script, which the CSP forbids", () => {
    expect(html).not.toMatch(/<script(?![^>]*\bsrc=)[^>]*>/)
  })

  it("loads nothing from another origin", () => {
    expect(html).not.toMatch(/https?:\/\//)
  })

  it("is titled ghr", () => {
    expect(html).toContain("<title>ghr</title>")
  })
})
