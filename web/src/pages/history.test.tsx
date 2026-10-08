import { screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { json, mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const secureContext = Object.getOwnPropertyDescriptor(window, "isSecureContext")

afterEach(() => {
  Reflect.deleteProperty(navigator, "clipboard")
  if (secureContext) Object.defineProperty(window, "isSecureContext", secureContext)
  else Reflect.deleteProperty(window, "isSecureContext")
})

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
    expect(calls.find((c) => c.path === "/api/history")?.search).toBe("?repo=&conclusion=&limit=500")
  })

  it("filters by repo", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user } = renderApp("/history")
    await screen.findByText("build")
    await user.click(screen.getByRole("combobox", { name: "Repo" }))
    await user.click(await screen.findByRole("option", { name: "darkmem" }))
    await waitFor(() => expect(calls.some((c) => c.search === "?repo=darkmem&conclusion=&limit=500")).toBe(true))
  })

  it("filters by result", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user } = renderApp("/history")
    await screen.findByText("build")
    await user.click(screen.getByRole("combobox", { name: "Result" }))
    await user.click(await screen.findByRole("option", { name: "failure" }))
    await waitFor(() => expect(calls.some((c) => c.search === "?repo=&conclusion=failure&limit=500")).toBe(true))
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

  it("copies a run's URL", async () => {
    mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user } = renderApp("/history")
    // userEvent.setup() (inside renderApp) installs its own clipboard stub, so the mock goes in after it.
    Object.defineProperty(window, "isSecureContext", { value: true, configurable: true })
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    await user.click(await screen.findByRole("button", { name: "Copy URL of build #41" }))
    expect(writeText).toHaveBeenCalledWith("https://github.com/darkraise/darkmem/actions/runs/101/job/1")
    expect((await screen.findAllByText("copied run URL")).length).toBeGreaterThan(0)
  })
})
