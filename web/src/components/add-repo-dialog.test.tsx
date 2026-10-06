import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

function routes(over: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/history": [], "GET /api/repos/available": fixtures.availableRepos, ...over })
}

async function open() {
  const view = renderApp("/repositories")
  await view.user.click(await screen.findByRole("button", { name: "+ Add repository" }))
  const dialog = within(await screen.findByRole("dialog"))
  await dialog.findByRole("option", { name: /new-repo/ })
  return { ...view, dialog }
}

describe("Add repository dialog", () => {
  it("lists the token's repos, with configured ones not pickable", async () => {
    mockApi(routes())
    const { dialog, user } = await open()
    expect(dialog.getByRole("option", { name: /darkmem/ })).toBeDisabled()
    expect(dialog.getByText("added")).toBeInTheDocument()
    expect(dialog.getByRole("button", { name: "Add" })).toBeDisabled()
    expect(dialog.getByText("pick one below")).toBeInTheDocument()
    expect(dialog.getByPlaceholderText("1 (default)")).toBeInTheDocument()
    expect(dialog.getByText("⚠ self-hosted runners on a public repo can run anyone's code")).toBeInTheDocument()
    await user.type(dialog.getByRole("textbox", { name: "Filter repositories" }), "zzz")
    expect(dialog.getByText("no match")).toBeInTheDocument()
  })

  it("says when the token sees no repo", async () => {
    mockApi(routes({ "GET /api/repos/available": [] }))
    const { user } = renderApp("/repositories")
    await user.click(await screen.findByRole("button", { name: "+ Add repository" }))
    expect(await within(await screen.findByRole("dialog")).findByText("nothing to pick")).toBeInTheDocument()
  })

  it("adds a repo with only the fields that were set", async () => {
    const { calls } = mockApi(routes({ "POST /api/repos": () => noContent() }))
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    await user.click(dialog.getByRole("button", { name: "Add" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ name: "new-repo", allow_public: false })
    expect((await screen.findAllByText("added new-repo")).length).toBeGreaterThan(0)
  })

  it("sends max, labels and the public override when set", async () => {
    const { calls } = mockApi(routes({ "POST /api/repos": () => noContent() }))
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    await user.type(dialog.getByLabelText("Max"), "2")
    await user.type(dialog.getByRole("textbox", { name: "Labels" }), "gpu{Enter}")
    await user.click(dialog.getByRole("switch", { name: "Allow public repo" }))
    await user.click(dialog.getByRole("button", { name: "Add" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true))
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ name: "new-repo", max: 2, labels: ["gpu"], allow_public: true })
  })

  it("keeps the dialog open with the daemon's reason on failure", async () => {
    mockApi(routes({ "POST /api/repos": () => json({ error: "repo new-repo is already configured" }, 409) }))
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    await user.click(dialog.getByRole("button", { name: "Add" }))
    expect(await dialog.findByText("✖ repo new-repo is already configured")).toBeInTheDocument()
    expect(screen.getByRole("dialog")).toBeInTheDocument()
  })

  it("is offered from the empty page too", async () => {
    mockApi(routes({ "GET /api/status": { ...fixtures.status, repos: [] } }))
    renderApp("/repositories")
    await screen.findByText("No repositories yet")
    expect(screen.getAllByRole("button", { name: "+ Add repository" })).toHaveLength(2)
  })
})
