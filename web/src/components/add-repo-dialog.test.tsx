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
  await view.user.click(await screen.findByRole("button", { name: "Add repository" }))
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
    expect(dialog.getByText("Pick one below")).toBeInTheDocument()
    expect(dialog.getByPlaceholderText("1 (default)")).toBeInTheDocument()
    expect(dialog.queryByRole("switch")).toBeNull()
    await user.type(dialog.getByRole("textbox", { name: "Filter repositories" }), "zzz")
    expect(dialog.getByText("No match")).toBeInTheDocument()
  })

  it.each([
    ["queue", "2"],
    ["all", "1"],
  ])("steps an empty Max from the %s mode default", async (mode, shown) => {
    mockApi(routes({ "GET /api/status": { ...fixtures.status, mode } }))
    const { dialog, user } = await open()
    await user.click(dialog.getByLabelText("Max"))
    await user.keyboard("{ArrowUp}")
    expect(dialog.getByLabelText("Max")).toHaveValue(shown)
  })

  it("keeps a watched repo pickable, since adding takes it over", async () => {
    mockApi(routes())
    const { dialog } = await open()
    expect(dialog.getByRole("option", { name: /docs/ })).toBeEnabled()
    expect(dialog.getByText("watched")).toBeInTheDocument()
  })

  it("sends Add while the daemon is unreachable and shows the request's error", { timeout: 10_000 }, async () => {
    let down = false
    const { calls } = mockApi(
      routes({
        "GET /api/status": () => (down ? json({ error: "connection refused" }, 502) : fixtures.status),
        "POST /api/repos": () => json({ error: "connection refused" }, 502),
      }),
    )
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    down = true
    await waitFor(() => expect(screen.getAllByText(/Daemon unreachable/).length).toBeGreaterThan(0), { timeout: 4000 })
    await user.click(dialog.getByRole("button", { name: "Add" }))
    expect(await dialog.findByText("connection refused")).toBeInTheDocument()
    expect(calls.some((c) => c.method === "POST")).toBe(true)
  })

  it("does not show an error from a request that ended after the dialog closed", async () => {
    let answer: (r: Response) => void = () => undefined
    mockApi(routes({ "POST /api/repos": () => new Promise<Response>((resolve) => (answer = resolve)) }))
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    await user.click(dialog.getByRole("button", { name: "Add" }))
    await user.click(dialog.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    answer(json({ error: "repo new-repo is already configured" }, 409))
    await user.click(screen.getAllByRole("button", { name: "Add repository" })[0] as HTMLElement)
    const again = within(await screen.findByRole("dialog"))
    await again.findByRole("option", { name: /new-repo/ })
    expect(again.queryByText(/already configured/)).toBeNull()
    expect(again.getByText("Pick one below")).toBeInTheDocument()
  })

  it("keeps a reopened dialog open when the earlier request succeeds", async () => {
    let answer: (r: Response) => void = () => undefined
    mockApi(routes({ "POST /api/repos": () => new Promise<Response>((resolve) => (answer = resolve)) }))
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    await user.click(dialog.getByRole("button", { name: "Add" }))
    await user.click(dialog.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    await user.click(screen.getAllByRole("button", { name: "Add repository" })[0] as HTMLElement)
    const again = within(await screen.findByRole("dialog"))
    await again.findByRole("option", { name: /new-repo/ })
    answer(noContent())
    expect((await screen.findAllByText("Added new-repo")).length).toBeGreaterThan(0)
    await expect(waitFor(() => expect(screen.queryByRole("dialog")).toBeNull(), { timeout: 500 })).rejects.toThrow()
  })

  it("moves between the pickable repos with the arrow keys, Home and End", async () => {
    const repos = [
      { name: "alpha", private: true, configured: false },
      { name: "darkmem", private: true, configured: true },
      { name: "gamma", private: false, configured: false },
      { name: "new-repo", private: true, configured: false },
    ]
    mockApi(routes({ "GET /api/repos/available": repos }))
    const { dialog, user } = await open()
    const option = (name: string) => dialog.getByRole("option", { name: new RegExp(`^${name}`) })
    expect(within(option("gamma")).getByText("public")).toHaveClass("text-muted-foreground")
    expect(within(option("alpha")).queryByText("private")).toBeNull()
    option("alpha").focus()
    await user.keyboard("{ArrowDown}")
    expect(option("gamma")).toHaveFocus()
    await user.keyboard("{ArrowUp}")
    expect(option("alpha")).toHaveFocus()
    await user.keyboard("{End}")
    expect(option("new-repo")).toHaveFocus()
    await user.keyboard("{Home}")
    expect(option("alpha")).toHaveFocus()
    await user.keyboard("{ArrowDown}{Enter}")
    expect(option("gamma")).toHaveAttribute("aria-selected", "true")
    expect(dialog.getByText("gamma", { selector: "strong" })).toBeInTheDocument()
  })

  it("keeps the list's status text outside the listbox", async () => {
    mockApi(routes())
    const { dialog, user } = await open()
    await user.type(dialog.getByRole("textbox", { name: "Filter repositories" }), "zzz")
    expect(dialog.getByText("No match")).toBeInTheDocument()
    expect(dialog.queryByRole("listbox")).toBeNull()
  })

  it("says when the token sees no repo", async () => {
    mockApi(routes({ "GET /api/repos/available": [] }))
    const { user } = renderApp("/repositories")
    await user.click(await screen.findByRole("button", { name: "Add repository" }))
    expect(await within(await screen.findByRole("dialog")).findByText("Nothing to pick")).toBeInTheDocument()
  })

  it("adds a repo with only the fields that were set", async () => {
    const { calls } = mockApi(routes({ "POST /api/repos": () => noContent() }))
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    await user.click(dialog.getByRole("button", { name: "Add" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ name: "new-repo", allow_public: false })
    expect((await screen.findAllByText("Added new-repo")).length).toBeGreaterThan(0)
  })

  it("sends max and labels when set", async () => {
    const { calls } = mockApi(routes({ "POST /api/repos": () => noContent() }))
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    await user.type(dialog.getByLabelText("Max"), "2")
    await user.type(dialog.getByRole("textbox", { name: "Labels" }), "gpu{Enter}")
    await user.click(dialog.getByRole("button", { name: "Add" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true))
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ name: "new-repo", max: 2, labels: ["gpu"], allow_public: false })
  })

  it("adds a public repo only with Serve when public on, after asking", async () => {
    const { calls } = mockApi(routes({ "POST /api/repos": () => noContent() }))
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /docs/ }))
    const serve = dialog.getByRole("switch", { name: "Serve when public" })
    expect(serve).not.toBeChecked()
    expect(dialog.getByText("A job in a public repository can come from anyone's pull request and runs on this host.")).toBeInTheDocument()
    expect(dialog.getByText("Turn on Serve when public to add a public repository.")).toBeInTheDocument()
    expect(dialog.getByRole("button", { name: "Add" })).toBeDisabled()
    await user.click(serve)
    await user.click(dialog.getByRole("button", { name: "Add" }))
    const question = within(await screen.findByRole("alertdialog"))
    expect(question.getByText("Serve docs while it is public?")).toBeInTheDocument()
    await user.click(question.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "POST")).toBe(false)
    expect(screen.getByRole("dialog")).toBeInTheDocument()
    await user.click(dialog.getByRole("button", { name: "Add" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Serve when public" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true))
    expect(calls.find((c) => c.method === "POST")?.body).toEqual({ name: "docs", allow_public: true })
  })

  it("turns Serve when public off again when another repo is picked", async () => {
    mockApi(routes())
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /docs/ }))
    await user.click(dialog.getByRole("switch", { name: "Serve when public" }))
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    expect(dialog.queryByRole("switch")).toBeNull()
    await user.click(dialog.getByRole("option", { name: /docs/ }))
    expect(dialog.getByRole("switch", { name: "Serve when public" })).not.toBeChecked()
  })

  it("keeps the dialog open with the daemon's reason on failure", async () => {
    mockApi(routes({ "POST /api/repos": () => json({ error: "repo new-repo is already configured" }, 409) }))
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    await user.click(dialog.getByRole("button", { name: "Add" }))
    expect(await dialog.findByText("repo new-repo is already configured")).toBeInTheDocument()
    expect(screen.getByRole("dialog")).toBeInTheDocument()
  })

  it("is offered from the empty page too", async () => {
    mockApi(routes({ "GET /api/status": { ...fixtures.status, repos: [] } }))
    renderApp("/repositories")
    await screen.findByText("No repositories yet")
    expect(screen.getAllByRole("button", { name: "Add repository" })).toHaveLength(1)
  })

  it("shows a busy Add while the request runs", async () => {
    mockApi(routes({ "POST /api/repos": () => new Promise<Response>(() => {}) }))
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    await user.click(dialog.getByRole("button", { name: "Add" }))
    // The kit's loading state marks the button aria-busy and keeps its label.
    await waitFor(() => expect(dialog.getAllByRole("button").filter((b) => b.getAttribute("aria-busy") === "true")).toHaveLength(1))
    expect(dialog.getAllByRole("button").find((b) => b.getAttribute("aria-busy") === "true")).toBeDisabled()
    expect(dialog.queryByText(/Adding/)).toBeNull()
  })
})
