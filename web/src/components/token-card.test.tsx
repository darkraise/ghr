import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { TokenStatus } from "@/api/types"
import { dateTime } from "@/lib/format"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const DAY = 86_400_000
// The UI runs on the daemon's clock, the status fixture's now.
const DAEMON_NOW = Date.parse(fixtures.status.now)

function token(over: Partial<TokenStatus> = {}): TokenStatus {
  return {
    ...fixtures.token,
    checked_at: new Date(DAEMON_NOW - 60_000).toISOString(),
    expires_at: new Date(DAEMON_NOW + 60 * DAY + 3_600_000).toISOString(),
    ...over,
  }
}

describe("GitHub token card", () => {
  it("shows the token's state, expiry and rate limit", async () => {
    const t = token()
    mockApi(authedRoutes({ "GET /api/token": t }))
    renderApp("/settings")
    expect(await screen.findByText(`expires ${dateTime(t.expires_at ?? "").slice(0, 10)} (60 days)`)).toBeInTheDocument()
    expect(screen.getByText("ok")).toBeInTheDocument()
    expect(screen.getByText("· checked 1m ago")).toBeInTheDocument()
    expect(screen.getByText("4980 / 5000, resets 14:45")).toBeInTheDocument()
  })

  it("warns when the token expires within 14 days", async () => {
    mockApi(authedRoutes({ "GET /api/token": token({ expires_at: new Date(DAEMON_NOW + 10 * DAY).toISOString() }) }))
    renderApp("/settings")
    expect(await screen.findByText("expires soon")).toBeInTheDocument()
  })

  it("shows a rejected token's reason", async () => {
    mockApi(authedRoutes({ "GET /api/token": { state: "rejected", reason: "Bad credentials" } }))
    renderApp("/settings")
    expect(await screen.findByText("Bad credentials")).toBeInTheDocument()
    expect(screen.getByText("rejected")).toBeInTheDocument()
    expect(screen.getByText("expiry unknown")).toBeInTheDocument()
  })

  it("offers a retry when the token cannot be read", async () => {
    let down = true
    mockApi(authedRoutes({ "GET /api/token": () => (down ? json({ error: "daemon busy" }, 500) : token()) }))
    const { user } = renderApp("/settings")
    expect(await screen.findByText("✖ daemon busy")).toBeInTheDocument()
    down = false
    await user.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByText("· checked 1m ago")).toBeInTheDocument()
  })

  it("replaces the token only after confirmation, sending it raw", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/token": token(), "PUT /api/token": () => noContent() }))
    const { user } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Replace token" }))
    const dialog = await screen.findByRole("dialog")
    await user.click(within(dialog).getByRole("button", { name: "Replace" }))
    expect(within(dialog).getByText("paste the new token first")).toBeInTheDocument()

    await user.type(within(dialog).getByLabelText("Token"), "github_pat_new")
    await user.click(within(dialog).getByRole("button", { name: "Replace" }))
    const ask = await screen.findByRole("alertdialog")
    expect(within(ask).getByText("Replace the GitHub token? ghr uses the new one at once.")).toBeInTheDocument()
    await user.click(within(ask).getByRole("button", { name: "Cancel" }))
    expect(calls.some((c) => c.method === "PUT")).toBe(false)

    await user.click(await within(dialog).findByRole("button", { name: "Replace" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Replace" }))
    await waitFor(() => expect(calls.some((c) => c.method === "PUT")).toBe(true))
    expect(calls.find((c) => c.method === "PUT")).toMatchObject({ path: "/api/token", body: "github_pat_new", contentType: "text/plain" })
    expect((await screen.findAllByText("GitHub token replaced")).length).toBeGreaterThan(0)
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  })

  it("shows a rejected token inside the dialog", async () => {
    mockApi(
      authedRoutes({
        "GET /api/token": token(),
        "PUT /api/token": () => json({ error: "new token rejected: Bad credentials" }, 400),
      }),
    )
    const { user } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Replace token" }))
    const dialog = await screen.findByRole("dialog")
    await user.type(within(dialog).getByLabelText("Token"), "github_pat_bad")
    await user.click(within(dialog).getByRole("button", { name: "Replace" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Replace" }))
    expect(await screen.findByText("✖ new token rejected: Bad credentials")).toBeInTheDocument()
  })
})
