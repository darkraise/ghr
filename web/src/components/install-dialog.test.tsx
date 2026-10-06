import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const goChoices = [{ spec: "1.23.4", version: "1.23.4" }]

const routes = (over: Record<string, unknown> = {}) =>
  authedRoutes({
    "GET /api/storage": fixtures.storage,
    "GET /api/toolchains/available": ({ url }: { url: URL }) =>
      url.searchParams.get("tool") === "node" ? fixtures.toolchainChoices : goChoices,
    ...over,
  })

async function openDialog(user: ReturnType<typeof renderApp>["user"]) {
  await user.click(await screen.findByRole("button", { name: "Install…" }))
  return within(await screen.findByRole("dialog"))
}

describe("Install toolchain dialog", () => {
  it("lists a tool's versions and installs the picked one", async () => {
    const { calls } = mockApi(routes({ "POST /api/toolchains": () => noContent() }))
    const { user } = renderApp("/storage")
    const dialog = await openDialog(user)
    expect(dialog.getByText("Install toolchain")).toBeInTheDocument()
    expect(dialog.getByText("pick one below, or type a version")).toBeInTheDocument()
    expect(await dialog.findByText("lts")).toBeInTheDocument()
    await user.click(dialog.getByRole("button", { name: /^22\.11\.0/ }))
    await user.click(dialog.getByRole("button", { name: "Install" }))
    await waitFor(() =>
      expect(calls.find((c) => c.method === "POST" && c.path === "/api/toolchains")?.body).toEqual({ tool: "node", version: "22.11.0" }),
    )
    expect((await screen.findAllByText("queued: install node 22.11.0")).length).toBeGreaterThan(0)
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  })

  it("switches the tool and installs a typed version", async () => {
    const { calls } = mockApi(routes({ "POST /api/toolchains": () => noContent() }))
    const { user } = renderApp("/storage")
    const dialog = await openDialog(user)
    await user.click(dialog.getByRole("combobox", { name: "Tool" }))
    await user.click(await screen.findByRole("option", { name: "Go" }))
    expect(await dialog.findByRole("button", { name: /^1\.23\.4/ })).toBeInTheDocument()
    expect(calls.some((c) => c.path === "/api/toolchains/available" && c.search === "?tool=go")).toBe(true)
    await user.type(dialog.getByRole("textbox", { name: "Version" }), "1.22")
    expect(dialog.getByText("no match")).toBeInTheDocument()
    await user.click(dialog.getByRole("button", { name: "Install" }))
    await waitFor(() =>
      expect(calls.find((c) => c.method === "POST" && c.path === "/api/toolchains")?.body).toEqual({ tool: "go", version: "1.22" }),
    )
  })

  it("offers a retry when the versions cannot be read", async () => {
    mockApi(routes({ "GET /api/toolchains/available": () => json({ error: "upstream down" }, 502) }))
    const { user } = renderApp("/storage")
    const dialog = await openDialog(user)
    expect(await dialog.findByText("✖ upstream down")).toBeInTheDocument()
    expect(dialog.getByRole("button", { name: "Retry" })).toBeInTheDocument()
    expect(dialog.getByRole("button", { name: "Install" })).toBeDisabled()
  })

  it("keeps a rejected install in the dialog", async () => {
    mockApi(routes({ "POST /api/toolchains": () => json({ error: "unknown toolchain \"node\"" }, 400) }))
    const { user } = renderApp("/storage")
    const dialog = await openDialog(user)
    await user.type(dialog.getByRole("textbox", { name: "Version" }), "24")
    await user.click(dialog.getByRole("button", { name: "Install" }))
    expect(await dialog.findByText('✖ unknown toolchain "node"')).toBeInTheDocument()
    expect(screen.getByRole("dialog")).toBeInTheDocument()
  })
})
