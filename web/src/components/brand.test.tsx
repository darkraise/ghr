import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
import { renderApp } from "@/test/render"
import { Brand } from "./brand"

describe("Brand", () => {
  it("shows the mark beside the wordmark", () => {
    const { container } = render(<Brand />)
    expect(screen.getByText("ghr")).toHaveClass("font-mono")
    expect(container.querySelector("svg")).toHaveAttribute("aria-hidden", "true")
  })

  it("heads the login page", async () => {
    mockApi({ "GET /auth/state": { setup_required: false, authenticated: false } })
    renderApp("/login")
    expect(await screen.findByText("ghr")).toHaveClass("font-mono")
    expect(screen.getAllByText("Log in")).toHaveLength(2)
  })
})
