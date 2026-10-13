import { screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { keys } from "@/api/hooks"
import type { StorageAlert } from "@/api/types"
import { mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const WARN = "GitHub artifact storage is at 72% of October's allowance and runs out around Oct 6."
const CRITICAL = "GitHub artifact storage is at 91% of October's allowance and runs out around Oct 6."
const EXHAUSTED = "GitHub artifact storage for October is used up: uploads fail until Nov 1."
const PROJECTED = "GitHub artifact storage runs out around Oct 6, before the month resets on Nov 1."

function routes(over: Partial<StorageAlert> | null) {
  const alert = over && { ...fixtures.storageAlert, ...over }
  return authedRoutes({ "GET /api/status": { ...fixtures.status, ...(alert ? { github_storage: alert } : {}) } })
}

beforeEach(() => localStorage.clear())
afterEach(() => vi.restoreAllMocks())

describe("quota banner", () => {
  it.each<[Partial<StorageAlert>, string, string, boolean]>([
    [{ level: "projected", used_pct: 40 }, PROJECTED, "border-warning", true],
    [{ level: "warn", used_pct: 72 }, WARN, "border-warning", true],
    [{ level: "critical" }, CRITICAL, "border-destructive", false],
    [{ level: "exhausted", used_pct: 103 }, EXHAUSTED, "border-destructive", false],
  ])("shows %j with its style, and a Dismiss button only for the early levels", async (over, title, border, dismissable) => {
    mockApi(routes(over))
    renderApp("/")
    const banner = await screen.findByRole("region", { name: title })
    expect(banner).toHaveClass(border)
    expect(within(banner).getByRole("link", { name: "Go to Artifacts" })).toHaveAttribute("href", "/artifacts")
    expect(within(banner).queryByRole("button", { name: "Dismiss" }) !== null).toBe(dismissable)
  })

  it.each([
    ["no quota", null],
    ["no level", { level: "" }],
  ])("is absent with %s", async (_why, over) => {
    mockApi(routes(over))
    const { queryClient } = renderApp("/")
    await waitFor(() => expect(queryClient.getQueryData(keys.status)).toBeDefined())
    expect(screen.queryByRole("region", { name: /^GitHub artifact storage/ })).toBeNull()
  })

  it("stays dismissed for that level and month, and comes back at the next level", async () => {
    mockApi(routes({ level: "warn", used_pct: 72 }))
    const { user, unmount } = renderApp("/")
    const banner = await screen.findByRole("region", { name: WARN })
    await user.click(within(banner).getByRole("button", { name: "Dismiss" }))
    await waitFor(() => expect(screen.queryByRole("region", { name: WARN })).toBeNull())
    unmount()

    mockApi(routes({ level: "warn", used_pct: 72 }))
    const again = renderApp("/")
    await waitFor(() => expect(again.queryClient.getQueryData(keys.status)).toBeDefined())
    expect(screen.queryByRole("region", { name: WARN })).toBeNull()
    again.unmount()

    mockApi(routes({ level: "critical" }))
    renderApp("/")
    expect(await screen.findByRole("region", { name: CRITICAL })).toBeInTheDocument()
  })

  it("can still be dismissed for this visit without local storage", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied")
    })
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("denied")
    })
    mockApi(routes({ level: "warn", used_pct: 72 }))
    const { user } = renderApp("/")
    const banner = await screen.findByRole("region", { name: WARN })
    await user.click(within(banner).getByRole("button", { name: "Dismiss" }))
    await waitFor(() => expect(screen.queryByRole("region", { name: WARN })).toBeNull())
  })

  it("opens the Artifacts page", async () => {
    mockApi(routes({ level: "exhausted", used_pct: 103 }))
    const { user } = renderApp("/")
    const banner = await screen.findByRole("region", { name: EXHAUSTED })
    await user.click(within(banner).getByRole("link", { name: "Go to Artifacts" }))
    expect(await screen.findByRole("heading", { level: 1, name: "Artifacts" })).toBeInTheDocument()
  })
})
