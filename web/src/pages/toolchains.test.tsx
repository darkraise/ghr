import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Toolchain } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const routes = (over: Record<string, unknown> = {}) => authedRoutes({ "GET /api/storage": fixtures.storage, ...over })

const sdk = (version: string): Toolchain => ({ tool: "dotnet", version, arch: "x64", path: "", bytes: 1, installed_at: "2026-10-01T00:00:00Z" })

const installed = () => within(screen.getByRole("region", { name: "Installed" }))

describe("Toolchains page", () => {
  it("offers Install and the popular set in the header", async () => {
    mockApi(routes())
    renderApp("/toolchains")
    expect(await screen.findByRole("heading", { name: "Toolchains", level: 1 })).toBeInTheDocument()
    expect(await screen.findByRole("button", { name: "Install" })).toBeEnabled()
    expect(screen.getByRole("button", { name: "Install popular set" })).toBeEnabled()
  })

  it("groups the installed toolchains by tool", async () => {
    mockApi(routes())
    renderApp("/toolchains")
    await screen.findByRole("region", { name: "Installed" })
    expect(installed().getByText("node", { selector: "th" })).toHaveAttribute("scope", "rowgroup")
    const row = within(installed().getByText("22.11.0").closest("tr") as HTMLElement)
    expect(row.getByText("22.11.0")).toHaveClass("font-mono")
    expect(row.getByText("x64")).toHaveClass("text-muted-foreground")
    expect(row.getByText("190.0 MB")).toHaveClass("font-mono")
    expect(row.getByText("2026-09-30")).toHaveClass("font-mono")
    expect(row.getByRole("button", { name: "Remove node 22.11.0" })).toBeEnabled()
  })

  it("lists the folders jobs installed, without actions", async () => {
    mockApi(routes())
    renderApp("/toolchains")
    await screen.findByRole("region", { name: "Installed" })
    expect(installed().getByText("Installed by jobs")).toBeInTheDocument()
    expect(installed().getByText("A job's own setup step installed these. ghr does not manage them.")).toBeInTheDocument()
    expect(installed().getByText("PyPy")).toBeInTheDocument()
    expect(installed().getByText("80.0 MB")).toHaveClass("font-mono")
    expect(installed().queryByRole("button", { name: /PyPy/ })).toBeNull()
  })

  it("shows the running operation and the refusal while jobs run", async () => {
    mockApi(routes())
    renderApp("/toolchains")
    expect(await screen.findByText("Installing node 24, extracting, 1 more queued")).toBeInTheDocument()
    expect(screen.getByText("Remove: refused while 1 job runs")).toHaveClass("text-muted-foreground")
  })

  it("says when the tool cache is empty, with the popular set", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, toolchains: null, other_tool_cache: [] } }))
    renderApp("/toolchains")
    expect(await screen.findByText("The tool cache is empty")).toBeInTheDocument()
    expect(screen.getAllByRole("button", { name: "Install popular set" })).toHaveLength(2)
  })

  it("shows a failed read with a retry", async () => {
    mockApi(routes({ "GET /api/storage": () => json({ error: "storage unavailable" }, 500) }))
    renderApp("/toolchains")
    expect(await screen.findByText("storage unavailable")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument()
    expect(screen.queryByText("Loading")).toBeNull()
  })

  it("removes a toolchain only once confirmed", async () => {
    const { calls } = mockApi(routes({ "DELETE /api/toolchains/node/22.11.0": () => noContent() }))
    const { user } = renderApp("/toolchains")
    await user.click(await screen.findByRole("button", { name: "Remove node 22.11.0" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText("Remove node 22.11.0?")).toBeInTheDocument()
    expect(ask.getByText("Frees 190.0 MB. Jobs that need it install it again.")).toBeInTheDocument()
    await user.click(ask.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)

    await user.click(screen.getByRole("button", { name: "Remove node 22.11.0" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/toolchains/node/22.11.0")).toBe(true))
    expect((await screen.findAllByText("Queued: remove node 22.11.0")).length).toBeGreaterThan(0)
  })

  it("warns in the body when removing the last .NET SDK of a major", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, toolchains: [sdk("8.0.404")] } }))
    const { user } = renderApp("/toolchains")
    await user.click(await screen.findByRole("button", { name: "Remove dotnet 8.0.404" }))
    expect(
      await screen.findByText("Frees 1 B. Jobs that need it install it again. It is the last .NET 8 SDK, so this also removes the 8.0 runtimes and packs."),
    ).toBeInTheDocument()
  })

  it("installs the popular set only once confirmed", async () => {
    const { calls } = mockApi(routes({ "POST /api/toolchains": () => noContent() }))
    const { user } = renderApp("/toolchains")
    await user.click(await screen.findByRole("button", { name: "Install popular set" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText("Install the popular set?")).toBeInTheDocument()
    await user.click(ask.getByRole("button", { name: "Install popular set" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ preset: "popular" }))
  })

  it("names the remove button in a tooltip", async () => {
    mockApi(routes())
    const { user } = renderApp("/toolchains")
    await user.hover(await screen.findByRole("button", { name: "Remove node 22.11.0" }))
    expect(await screen.findByRole("tooltip")).toHaveTextContent("Remove")
  })
})
