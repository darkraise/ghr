import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { type Call, json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

async function draw(routes: Record<string, unknown> = {}) {
  const { calls } = mockApi(authedRoutes({ "PATCH /api/config": () => noContent(), ...routes }))
  const view = renderApp("/settings/github-storage")
  await screen.findByRole("region", { name: "Policy" })
  return { ...view, calls }
}

const field = (name: string) => screen.getByRole("spinbutton", { name }) as HTMLInputElement
const age = () => screen.getByRole("textbox", { name: "Cleanup age" }) as HTMLInputElement
const patches = (calls: Call[]) => calls.filter((c) => c.method === "PATCH")
// The alerts on the page itself, not the toasts that repeat them.
const pageAlerts = () =>
  screen
    .queryAllByRole("alert")
    .filter((a) => !a.closest("[data-sonner-toaster]"))
    .map((a) => a.textContent)

describe("GitHub storage section", () => {
  it("shows the saved policy with the defaults filled in", async () => {
    await draw()
    expect(field("Included storage").value).toBe("500")
    expect(field("Warning at").value).toBe("70")
    expect(field("Critical at").value).toBe("90")
    expect(age().value).toBe("2d")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
    expect(screen.getByRole("link", { name: "Artifacts page" })).toHaveAttribute("href", "/artifacts")
  })

  it("previews a new cleanup age and saves it once confirmed", async () => {
    const { user, calls } = await draw({
      "GET /api/github-storage/preview": [
        { repo: "darkcloud", count: 1, bytes: 20971520 },
        { repo: "darkmem", count: 2, bytes: 209715200 },
      ],
    })
    await user.clear(age())
    await user.type(age(), "1d")
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Clean artifacts after 1d?" })
    expect(dialog).toHaveTextContent("Cleanup does not protect an artifact that a different run downloads later")
    expect(dialog).toHaveTextContent("Up to 3 artifacts (230.7 MB) older than 1d would go at the next check.")
    expect(within(dialog).getByText("darkmem: up to 2 artifacts, 209.7 MB")).toBeInTheDocument()
    expect(calls.find((c) => c.path === "/api/github-storage/preview")?.search).toBe("?cleanup=1d")
    expect(patches(calls)).toHaveLength(0)
    await user.click(within(dialog).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(patches(calls).map((c) => c.body)).toEqual([{ github_storage: { cleanup_max_age: "1d" } }]))
    expect((await screen.findAllByText("Storage policy saved")).length).toBeGreaterThan(0)
  })

  it("keeps the edit and sends nothing when the preview is cancelled", async () => {
    const { user, calls } = await draw({ "GET /api/github-storage/preview": [] })
    await user.clear(age())
    await user.type(age(), "12h")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Clean artifacts after 12h?" })
    expect(dialog).toHaveTextContent("No artifact is older than 12h now.")
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(patches(calls)).toHaveLength(0)
    expect(age().value).toBe("12h")
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
  })

  it.each<[string, (u: Awaited<ReturnType<typeof draw>>["user"]) => Promise<void>, unknown]>([
    ["turns cleanup off without a preview", async (user) => user.clear(age()), { cleanup_max_age: "" }],
    [
      "saves the allowance and warning levels",
      async (user) => {
        await user.clear(field("Included storage"))
        await user.type(field("Included storage"), "1024")
        await user.clear(field("Warning at"))
        await user.type(field("Warning at"), "60")
        await user.clear(field("Critical at"))
        await user.type(field("Critical at"), "85")
      },
      { included_mb: 1024, warn_pct: [60, 85] },
    ],
  ])("%s", async (_name, edit, patch) => {
    const { user, calls } = await draw()
    await edit(user)
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(patches(calls).map((c) => c.body)).toEqual([{ github_storage: patch }]))
    expect(calls.some((c) => c.path === "/api/github-storage/preview")).toBe(false)
  })

  it("pulls Included storage 0 up to 1 once the field is left", async () => {
    const { user } = await draw()
    await user.clear(field("Included storage"))
    await user.type(field("Included storage"), "0")
    expect(await screen.findByText("Enter a whole number of MB, at least 1")).toBeInTheDocument()
    await user.tab()
    expect(field("Included storage")).toHaveValue("1")
    expect(screen.queryByText("Enter a whole number of MB, at least 1")).toBeNull()
  })

  it.each<[string, string, string]>([
    ["Warning at", "95", "Enter two whole percentages from 1 to 100, the first lower"],
  ])("refuses %s %s before sending", async (name, value, error) => {
    const { user, calls } = await draw()
    await user.clear(field(name))
    await user.type(field(name), value)
    expect(await screen.findByText(error)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Fix the highlighted settings first")).length).toBeGreaterThan(0)
    expect(patches(calls)).toHaveLength(0)
  })

  it("shows the daemon's refusal and keeps the edit", async () => {
    const msg = "github_storage.cleanup_max_age must be at least 24h"
    const { user } = await draw({ "GET /api/github-storage/preview": [], "PATCH /api/config": () => json({ error: msg }, 400) })
    await user.clear(age())
    await user.type(age(), "12h")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Clean artifacts after 12h?" })
    await user.click(within(dialog).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(pageAlerts()).toContain(msg))
    expect(age().value).toBe("12h")
  })

  it("shows a failed preview read on the page and sends nothing", async () => {
    const { user, calls } = await draw({ "GET /api/github-storage/preview": () => json({ error: "preview broke" }, 500) })
    await user.clear(age())
    await user.type(age(), "12h")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(pageAlerts()).toContain("preview broke"))
    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(patches(calls)).toHaveLength(0)
    expect(age().value).toBe("12h")
  })

  it("keeps the saved values when the config cannot be read back", async () => {
    let fail = false
    const { user } = await draw({ "GET /api/config": () => (fail ? json({ error: "config gone" }, 500) : fixtures.config) })
    fail = true
    await user.clear(field("Included storage"))
    await user.type(field("Included storage"), "1024")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText(/Saved, but re-reading the config failed/)).length).toBeGreaterThan(0)
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
    expect(field("Included storage").value).toBe("1024")
  })

  it("previews a changed cleanup age when Save is chosen on the leave prompt, and stays", async () => {
    const { user, router, calls } = await draw({ "GET /api/github-storage/preview": [] })
    await user.clear(age())
    await user.type(age(), "1d")
    await user.click(screen.getAllByRole("link", { name: "Dashboard" })[0] as HTMLElement)
    const prompt = await screen.findByRole("alertdialog", { name: "Unsaved changes" })
    await user.click(within(prompt).getByRole("button", { name: "Save" }))
    expect(await screen.findByRole("alertdialog", { name: "Clean artifacts after 1d?" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/settings/github-storage")
    expect(patches(calls)).toHaveLength(0)
  })

  it("asks before leaving with unsaved changes", async () => {
    const { user, router } = await draw()
    await user.clear(field("Included storage"))
    await user.type(field("Included storage"), "1024")
    await user.click(screen.getAllByRole("link", { name: "Dashboard" })[0] as HTMLElement)
    expect(await screen.findByText("You have 1 unsaved change on the GitHub storage page.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Stay" }))
    expect(router.state.location.pathname).toBe("/settings/github-storage")
  })
})
