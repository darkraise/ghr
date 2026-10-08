import { screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

describe("Toolchains page", () => {
  it("shows the toolchains card", async () => {
    mockApi(authedRoutes({ "GET /api/storage": fixtures.storage }))
    renderApp("/toolchains")
    expect(await screen.findByRole("heading", { name: "Toolchains", level: 1 })).toBeInTheDocument()
    expect(await screen.findByRole("button", { name: "Install popular set" })).toBeInTheDocument()
  })

  it("shows a failed read", async () => {
    mockApi(authedRoutes({ "GET /api/storage": () => json({ error: "storage unavailable" }, 500) }))
    renderApp("/toolchains")
    expect(await screen.findByText("✖ storage unavailable")).toBeInTheDocument()
    expect(screen.queryByText("loading…")).toBeNull()
  })
})
