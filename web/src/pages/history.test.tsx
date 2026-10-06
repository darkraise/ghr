import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

describe("History page", () => {
  it("lists finished jobs, newest first as served", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    renderApp("/history")
    expect(await screen.findByText("build")).toBeInTheDocument()
    expect(screen.getByText("deploy")).toBeInTheDocument()
    expect(screen.getByText("#41")).toBeInTheDocument()
    expect(screen.getByText("success")).toBeInTheDocument()
    expect(screen.getByText("failure")).toBeInTheDocument()
    expect(screen.getAllByRole("link", { name: "Open run" })).toHaveLength(1)
    expect(calls.find((c) => c.path === "/api/history")?.search).toBe("?repo=&conclusion=&limit=200")
  })

  it("filters by repo", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user } = renderApp("/history")
    await screen.findByText("build")
    await user.click(screen.getByRole("combobox", { name: "Repo" }))
    await user.click(await screen.findByRole("option", { name: "darkmem" }))
    await waitFor(() => expect(calls.some((c) => c.search === "?repo=darkmem&conclusion=&limit=200")).toBe(true))
  })

  it("filters by result", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user } = renderApp("/history")
    await screen.findByText("build")
    await user.click(screen.getByRole("combobox", { name: "Result" }))
    await user.click(await screen.findByRole("option", { name: "failure" }))
    await waitFor(() => expect(calls.some((c) => c.search === "?repo=&conclusion=failure&limit=200")).toBe(true))
  })

  it("says when nothing has finished", async () => {
    mockApi(authedRoutes({ "GET /api/history": [] }))
    renderApp("/history")
    expect(await screen.findByText("no finished jobs yet")).toBeInTheDocument()
  })

  it("shows a failed read", async () => {
    mockApi(authedRoutes({ "GET /api/history": () => json({ error: "history file unreadable" }, 500) }))
    renderApp("/history")
    expect(await screen.findByText("history file unreadable")).toBeInTheDocument()
  })

  it("shows loading until the first read answers", async () => {
    mockApi(authedRoutes({ "GET /api/history": () => new Promise(() => {}) }))
    renderApp("/history")
    expect(await screen.findByText("loading…")).toBeInTheDocument()
    expect(screen.queryByText("no finished jobs yet")).toBeNull()
  })
})
