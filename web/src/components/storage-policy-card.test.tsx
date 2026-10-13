import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Config } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { StoragePolicyCard } from "./storage-policy-card"

async function draw({ config = fixtures.config, busy = false }: { config?: Config; busy?: boolean } = {}) {
  const view = renderRoutes({ "/": () => <StoragePolicyCard storage={fixtures.githubStorage} config={config} offline={false} busy={busy} /> }, "/")
  await screen.findByRole("region", { name: "Policy" })
  return view
}

const field = (name: string) => screen.getByRole("spinbutton", { name }) as HTMLInputElement

describe("StoragePolicyCard", () => {
  it("shows the saved policy with the defaults filled in", async () => {
    mockApi({})
    await draw()
    expect(field("Included storage").value).toBe("500")
    expect(field("Warning at").value).toBe("70")
    expect(field("Critical at").value).toBe("90")
    expect((screen.getByRole("textbox", { name: "Cleanup age" }) as HTMLInputElement).value).toBe("2d")
    expect(screen.getByRole("button", { name: "Save policy" })).toBeDisabled()
  })

  it("previews a new cleanup age and saves it once confirmed", async () => {
    const { calls } = mockApi({
      "GET /api/github-storage/preview": [
        { repo: "darkcloud", count: 1, bytes: 20971520 },
        { repo: "darkmem", count: 2, bytes: 209715200 },
      ],
      "PATCH /api/config": () => noContent(),
      "GET /api/config": fixtures.config,
    })
    const { user } = await draw()
    const age = screen.getByRole("textbox", { name: "Cleanup age" })
    await user.clear(age)
    await user.type(age, "1d")
    await user.click(screen.getByRole("button", { name: "Save policy" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Clean artifacts after 1d?" })
    expect(dialog).toHaveTextContent("Cleanup does not protect an artifact that a different run downloads later")
    expect(dialog).toHaveTextContent("Up to 3 artifacts (230.7 MB) older than 1d would go at the next check.")
    expect(within(dialog).getByText("darkcloud: up to 1 artifact, 21.0 MB")).toBeInTheDocument()
    expect(within(dialog).getByText("darkmem: up to 2 artifacts, 209.7 MB")).toBeInTheDocument()
    expect(calls.find((c) => c.path === "/api/github-storage/preview")?.search).toBe("?cleanup=1d")
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
    await user.click(within(dialog).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ github_storage: { cleanup_max_age: "1d" } }))
    expect((await screen.findAllByText("Storage policy saved")).length).toBeGreaterThan(0)
  })

  it.each<[string, (u: Awaited<ReturnType<typeof draw>>["user"]) => Promise<void>, unknown]>([
    [
      "turns cleanup off without a preview",
      async (user) => user.clear(screen.getByRole("textbox", { name: "Cleanup age" })),
      { cleanup_max_age: "" },
    ],
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
    const { calls } = mockApi({ "PATCH /api/config": () => noContent(), "GET /api/config": fixtures.config })
    const { user } = await draw()
    await edit(user)
    await user.click(screen.getByRole("button", { name: "Save policy" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ github_storage: patch }))
    expect(calls.some((c) => c.path === "/api/github-storage/preview")).toBe(false)
  })

  it.each<[string, string, string]>([
    ["Included storage", "0", "Enter a whole number of MB, at least 1"],
    ["Warning at", "95", "Enter two whole percentages from 1 to 100, the first lower"],
  ])("refuses %s %s before sending", async (name, value, error) => {
    const { calls } = mockApi({})
    const { user } = await draw()
    await user.clear(field(name))
    await user.type(field(name), value)
    expect(await screen.findByText(error)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save policy" }))
    expect(calls).toHaveLength(0)
  })

  it("shows the daemon's refusal", async () => {
    const msg = "github_storage.cleanup_max_age must be at least 24h"
    mockApi({ "GET /api/github-storage/preview": [], "PATCH /api/config": json({ error: msg }, 400) })
    const { user } = await draw()
    const age = screen.getByRole("textbox", { name: "Cleanup age" })
    await user.clear(age)
    await user.type(age, "12h")
    await user.click(screen.getByRole("button", { name: "Save policy" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Clean artifacts after 12h?" })
    expect(dialog).toHaveTextContent("No artifact is older than 12h now.")
    await user.click(within(dialog).getByRole("button", { name: "Save" }))
    expect(await screen.findByRole("alert")).toHaveTextContent(msg)
  })

  it("sets retention on every repository after a warning", async () => {
    const { calls } = mockApi({ "POST /api/github-storage/retention": () => json({ id: "op-2" }, 202) })
    const { user } = await draw()
    await user.type(field("Retention for all"), "7")
    await user.click(screen.getByRole("button", { name: "Set retention" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Set retention to 7 days on 3 repositories?" })
    expect(dialog).toHaveTextContent(
      "GitHub's retention also limits how long it keeps runs and logs, which ghr's Actions and log pages read. A repository whose maximum is lower is skipped.",
    )
    await user.click(within(dialog).getByRole("button", { name: "Set retention" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ days: 7 }))
    expect((await screen.findAllByText("Retention change started")).length).toBeGreaterThan(0)
  })

  it("holds Set retention while an operation runs", async () => {
    mockApi({})
    const { user } = await draw({ busy: true })
    await user.type(field("Retention for all"), "7")
    expect(screen.getByRole("button", { name: "Set retention" })).toBeDisabled()
  })
})
