import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Toolchain } from "@/api/types"
import { mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const routes = (over: Record<string, unknown> = {}) => authedRoutes({ "GET /api/storage": fixtures.storage, ...over })

const sdk = (version: string): Toolchain => ({ tool: "dotnet", version, arch: "x64", path: "", bytes: 1, installed_at: "2026-10-01T00:00:00Z" })

describe("Toolchains card", () => {
  it("lists toolchains, other folders and the queue", async () => {
    mockApi(routes())
    renderApp("/toolchains")
    expect(await screen.findByText("node 22.11.0")).toBeInTheDocument()
    expect(screen.getByText("190.0 MB")).toBeInTheDocument()
    expect(screen.getByText("installed 2026-09-30")).toBeInTheDocument()
    expect(screen.getByText("PyPy")).toBeInTheDocument()
    expect(screen.getByText("other: a job's own setup step")).toBeInTheDocument()
    expect(screen.getByText("Installing node 24, extracting, 1 more queued")).toBeInTheDocument()
    expect(screen.getByText("Remove: refused while 1 jobs run")).toBeInTheDocument()
  })

  it("says when the tool cache is empty", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, toolchains: null, other_tool_cache: [] } }))
    renderApp("/toolchains")
    expect(await screen.findByText("the tool cache is empty")).toBeInTheDocument()
  })

  it("removes a toolchain only once confirmed", async () => {
    const { calls } = mockApi(routes({ "DELETE /api/toolchains/node/22.11.0": () => noContent() }))
    const { user } = renderApp("/toolchains")
    await user.click(await screen.findByRole("button", { name: "Remove node 22.11.0" }))
    expect(await screen.findByText("Remove node 22.11.0 (190.0 MB)?")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)

    await user.click(screen.getByRole("button", { name: "Remove node 22.11.0" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/toolchains/node/22.11.0")).toBe(true))
    expect((await screen.findAllByText("queued: remove node 22.11.0")).length).toBeGreaterThan(0)
  })

  it("warns when removing the last .NET SDK of a major", async () => {
    const storage = { ...fixtures.storage, toolchains: [sdk("8.0.404")] }
    mockApi(routes({ "GET /api/storage": storage }))
    const { user } = renderApp("/toolchains")
    await user.click(await screen.findByRole("button", { name: "Remove dotnet 8.0.404" }))
    expect(
      await screen.findByText("Remove dotnet 8.0.404 (1 B)? It is the last .NET 8 SDK, so this also removes the 8.0 runtimes and packs."),
    ).toBeInTheDocument()
  })

  it("installs the popular set only once confirmed", async () => {
    const { calls } = mockApi(routes({ "POST /api/toolchains": () => noContent() }))
    const { user } = renderApp("/toolchains")
    await user.click(await screen.findByRole("button", { name: "Install popular set" }))
    const question =
      "Install the popular set? node 22, node 24, dotnet 8.0, dotnet 10.0, python 3.13, python 3.14, go latest, java 21, java 25. Versions already installed are skipped."
    expect(await screen.findByText(question)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "POST")).toBe(false)

    await user.click(screen.getByRole("button", { name: "Install popular set" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Install popular set" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ preset: "popular" }))
    expect((await screen.findAllByText("queued: popular set")).length).toBeGreaterThan(0)
  })
})
