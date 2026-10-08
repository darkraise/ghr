import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { TokenStatus } from "@/api/types"
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

describe("GitHub token section", () => {
  it("shows the token's state, expiry and rate limit", async () => {
    mockApi(authedRoutes({ "GET /api/token": token() }))
    renderApp("/settings")
    expect(await screen.findByText("Expires Dec 2, in 60 days")).toBeInTheDocument()
    const section = within(screen.getByRole("region", { name: "GitHub token" }))
    expect(section.getByText("Valid")).toHaveClass("text-success")
    expect(section.getByText("1m ago")).toBeInTheDocument()
    expect(section.getByText("4,980 of 5,000, resets 14:45")).toHaveClass("font-mono")
  })

  it("warns when the token expires within 14 days", async () => {
    mockApi(authedRoutes({ "GET /api/token": token({ expires_at: new Date(DAEMON_NOW + 10 * DAY).toISOString() }) }))
    renderApp("/settings")
    expect(await screen.findByText("Expires soon")).toBeInTheDocument()
  })

  it("shows a rejected token's reason", async () => {
    mockApi(authedRoutes({ "GET /api/token": { state: "rejected", reason: "Bad credentials" } }))
    renderApp("/settings")
    expect(await screen.findByText("Bad credentials")).toBeInTheDocument()
    expect(screen.getByText("Rejected")).toBeInTheDocument()
    expect(screen.getByText("Expiry unknown")).toBeInTheDocument()
  })

  it("offers a retry when the token cannot be read", async () => {
    let down = true
    mockApi(authedRoutes({ "GET /api/token": () => (down ? json({ error: "daemon busy" }, 500) : token()) }))
    const { user } = renderApp("/settings")
    expect(await screen.findByText("daemon busy")).toBeInTheDocument()
    down = false
    await user.click(screen.getByRole("button", { name: "Try again" }))
    expect(await screen.findByText("1m ago")).toBeInTheDocument()
  })

  it("replaces the token only after confirmation, sending it raw", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/token": token(), "PUT /api/token": () => noContent() }))
    const { user } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Replace token" }))
    const dialog = await screen.findByRole("dialog")
    await user.click(within(dialog).getByRole("button", { name: "Replace" }))
    expect(within(dialog).getByText("Paste the new token first")).toBeInTheDocument()

    await user.type(within(dialog).getByLabelText("Token"), "github_pat_new")
    await user.click(within(dialog).getByRole("button", { name: "Replace" }))
    const ask = await screen.findByRole("alertdialog")
    expect(within(ask).getByText("Replace the GitHub token?")).toBeInTheDocument()
    expect(within(ask).getByText("ghr uses the new one at once.")).toBeInTheDocument()
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
    expect(await screen.findByText("new token rejected: Bad credentials")).toBeInTheDocument()
  })

  it("does not show an error from a replace that ended after the dialog closed", async () => {
    let answer: (r: Response) => void = () => undefined
    mockApi(authedRoutes({ "GET /api/token": token(), "PUT /api/token": () => new Promise<Response>((resolve) => (answer = resolve)) }))
    const { user } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Replace token" }))
    const dialog = await screen.findByRole("dialog")
    await user.type(within(dialog).getByLabelText("Token"), "github_pat_bad")
    await user.click(within(dialog).getByRole("button", { name: "Replace" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Replace" }))
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    answer(json({ error: "new token rejected: Bad credentials" }, 400))
    await user.click(screen.getByRole("button", { name: "Replace token" }))
    const again = await screen.findByRole("dialog")
    expect(within(again).getByLabelText("Token")).toHaveValue("")
    expect(screen.queryByText(/Bad credentials/)).toBeNull()
  })

  it("leaves the rate limit blank until it is known, and says when nothing is read yet", async () => {
    mockApi(authedRoutes({ "GET /api/token": () => new Promise(() => {}) }))
    renderApp("/settings")
    const section = within(await screen.findByRole("region", { name: "GitHub token" }))
    expect(section.getByText("Not read yet")).toBeInTheDocument()
    expect(section.queryByText("–")).toBeNull()
  })
})
