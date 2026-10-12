import { screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { setupStart } from "@/api/hooks"
import type { SetupState } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const base: SetupState = {
  configured: false,
  starting: false,
  setup_pending: true,
  toolchains_pending: true,
  owner: "",
  web_listen: "0.0.0.0:8080",
}

afterEach(() => {
  setupStart.limitMs = 60_000
})

async function submitGitHub(user: ReturnType<typeof renderApp>["user"], owner: string | null) {
  await screen.findByRole("heading", { name: "Set up ghr" })
  if (owner !== null) await user.type(screen.getByLabelText("GitHub owner"), owner)
  await user.type(screen.getByLabelText("Token"), "tok")
  await user.click(screen.getByRole("button", { name: "Save and start ghr" }))
}

describe("setup wizard", () => {
  it("saves owner and token, waits for ghr to start, then opens Repositories", { timeout: 15_000 }, async () => {
    let state = base
    let polls = 0
    const { calls } = mockApi(
      authedRoutes({
        "GET /api/setup": () => {
          if (state.starting && ++polls >= 2) state = { ...state, starting: false, configured: true }
          return state
        },
        "POST /api/setup/github": () => {
          state = { ...state, starting: true, owner: "DarkRaise" }
          return noContent()
        },
      }),
    )
    const { user } = renderApp("/setup")
    await submitGitHub(user, "darkraise")
    expect(await screen.findByText("Starting ghr…")).toBeInTheDocument()
    expect(await screen.findByRole("heading", { name: "Repositories" }, { timeout: 10_000 })).toBeInTheDocument()
    expect(calls.find((c) => c.path === "/api/setup/github")?.body).toEqual({ owner: "darkraise", token: "tok" })
  })

  it("asks for Actions: read/write on the GitHub step", async () => {
    mockApi(authedRoutes({ "GET /api/setup": base }))
    renderApp("/setup")
    await screen.findByRole("heading", { name: "Set up ghr" })
    expect(screen.getByText(/Actions: read\/write/)).toBeInTheDocument()
  })

  it("shows GitHub's refusal inline", async () => {
    mockApi(
      authedRoutes({
        "GET /api/setup": base,
        "POST /api/setup/github": () => json({ error: "token rejected by GitHub: github: 401 Bad credentials" }, 400),
      }),
    )
    const { user } = renderApp("/setup")
    await submitGitHub(user, "DarkRaise")
    expect(await screen.findByRole("alert")).toHaveTextContent("token rejected by GitHub: github: 401 Bad credentials")
  })

  it("keeps config.yaml's owner read-only and sends it empty", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/setup": { ...base, owner: "DarkRaise" }, "POST /api/setup/github": () => noContent() }))
    const { user } = renderApp("/setup")
    const owner = await screen.findByLabelText("GitHub owner")
    expect(owner).toHaveValue("DarkRaise")
    expect(owner).toHaveAttribute("readonly")
    await submitGitHub(user, null)
    await waitFor(() => expect(calls.find((c) => c.path === "/api/setup/github")?.body).toEqual({ owner: "", token: "tok" }))
  })

  it("says when ghr takes too long to start", async () => {
    setupStart.limitMs = 50
    let state = base
    mockApi(
      authedRoutes({
        "GET /api/setup": () => state,
        "POST /api/setup/github": () => {
          state = { ...state, starting: true }
          return noContent()
        },
      }),
    )
    const { user } = renderApp("/setup")
    await submitGitHub(user, "DarkRaise")
    expect(await screen.findByText("ghr is still starting; check journalctl -u ghr on the host")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument()
  })

  it("arms the start timeout when the page loads while ghr is starting", async () => {
    setupStart.limitMs = 50
    mockApi(authedRoutes({ "GET /api/setup": { ...base, starting: true } }))
    renderApp("/setup")
    expect(await screen.findByText("ghr is still starting; check journalctl -u ghr on the host")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument()
  })

  it("walks repositories, settings and toolchains, and finishes with the popular set", { timeout: 15_000 }, async () => {
    let finished: unknown
    const { calls } = mockApi(
      authedRoutes({
        "GET /api/setup": { ...base, configured: true, owner: "DarkRaise" },
        "GET /api/storage": fixtures.storage,
        "PATCH /api/config": () => noContent(),
        "POST /api/setup/finish": ({ body }: { body: unknown }) => {
          finished = body
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/setup")
    expect(await screen.findByRole("heading", { name: "Repositories", level: 2 })).toBeInTheDocument()
    expect(await screen.findByRole("list", { name: "Configured repositories" })).toHaveTextContent("darkmem")
    await user.click(screen.getByRole("button", { name: "Next" }))

    const max = await screen.findByLabelText("Max concurrent runners")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(screen.getByRole("button", { name: "Next" }))
    const dialog = within(await screen.findByRole("alertdialog"))
    await user.click(dialog.getByRole("button", { name: "Save" }))

    expect(await screen.findByRole("heading", { name: "Toolchains", level: 2 })).toBeInTheDocument()
    expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ global_max: 3 })
    expect(screen.getByRole("switch", { name: "Popular set" })).toBeChecked()
    await user.click(screen.getByRole("button", { name: "Next" }))
    await user.click(await screen.findByRole("button", { name: "Finish" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
    expect(finished).toEqual({ toolchains: "popular" })
  })

  it("asks before Back drops unsaved settings; Discard goes back", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/setup": { ...base, configured: true, owner: "DarkRaise" }, "GET /api/storage": fixtures.storage }))
    const { user } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Next" }))
    const max = await screen.findByLabelText("Max concurrent runners")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(screen.getByRole("button", { name: "Back" }))
    const dialog = within(await screen.findByRole("alertdialog"))
    expect(screen.queryByRole("heading", { name: "Repositories", level: 2 })).toBeNull()
    await user.click(dialog.getByRole("button", { name: "Discard" }))
    expect(await screen.findByRole("heading", { name: "Repositories", level: 2 })).toBeInTheDocument()
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
  })

  it("asks before Skip drops unsaved settings", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/setup": { ...base, configured: true, owner: "DarkRaise" }, "GET /api/storage": fixtures.storage }))
    const { user } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Next" }))
    const max = await screen.findByLabelText("Max concurrent runners")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(screen.getByRole("button", { name: "Skip the rest" }))
    const dialog = within(await screen.findByRole("alertdialog"))
    expect(calls.some((c) => c.path === "/api/setup/finish")).toBe(false)
    await user.click(dialog.getByRole("button", { name: "Stay" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(screen.getByLabelText("Max concurrent runners")).toHaveValue("3")
    expect(calls.some((c) => c.path === "/api/setup/finish")).toBe(false)
  })

  it("stays on the step when an edit is made while the dialog's save is pending", async () => {
    let release: () => void = () => {}
    const gate = new Promise<void>((resolve) => (release = resolve))
    const { calls } = mockApi(
      authedRoutes({
        "GET /api/setup": { ...base, configured: true, owner: "DarkRaise" },
        "GET /api/storage": fixtures.storage,
        "PATCH /api/config": async () => {
          await gate
          return noContent()
        },
      }),
    )
    const { user } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Next" }))
    const max = await screen.findByLabelText("Max concurrent runners")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(screen.getByRole("button", { name: "Next" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    const disk = screen.getByLabelText("Disk high-water")
    await user.clear(disk)
    await user.type(disk, "70")
    const reads = calls.filter((c) => c.path === "/api/config" && c.method === "GET").length
    release()
    await waitFor(() => expect(calls.filter((c) => c.path === "/api/config" && c.method === "GET").length).toBeGreaterThan(reads))
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(screen.getByLabelText("Disk high-water")).toHaveValue("70")
    expect(screen.queryByRole("heading", { name: "Toolchains", level: 2 })).toBeNull()
    expect(screen.getByRole("heading", { name: "Settings", level: 2 })).toBeInTheDocument()
  })

  it("stays on the step when Stay is chosen in a later prompt while the dialog's save is pending", async () => {
    let release: () => void = () => {}
    const gate = new Promise<void>((resolve) => (release = resolve))
    const { calls } = mockApi(
      authedRoutes({
        "GET /api/setup": { ...base, configured: true, owner: "DarkRaise" },
        "GET /api/storage": fixtures.storage,
        "PATCH /api/config": async () => {
          await gate
          return noContent()
        },
      }),
    )
    const { user } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Next" }))
    const max = await screen.findByLabelText("Max concurrent runners")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(screen.getByRole("button", { name: "Next" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    await user.click(screen.getByRole("button", { name: "Back" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Stay" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    const reads = calls.filter((c) => c.path === "/api/config" && c.method === "GET").length
    release()
    await waitFor(() => expect(calls.filter((c) => c.path === "/api/config" && c.method === "GET").length).toBeGreaterThan(reads))
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(screen.queryByRole("heading", { name: "Toolchains", level: 2 })).toBeNull()
    expect(screen.getByRole("heading", { name: "Settings", level: 2 })).toBeInTheDocument()
  })

  it("stays on the step and shows the error when the dialog's save fails", async () => {
    mockApi(
      authedRoutes({
        "GET /api/setup": { ...base, configured: true, owner: "DarkRaise" },
        "GET /api/storage": fixtures.storage,
        "PATCH /api/config": () => json({ error: "global_max is too high" }, 400),
      }),
    )
    const { user } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Next" }))
    const max = await screen.findByLabelText("Max concurrent runners")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(screen.getByRole("button", { name: "Next" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Save" }))
    expect(await screen.findByText(/global_max is too high/)).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Toolchains", level: 2 })).toBeNull()
    expect(screen.getByLabelText("Max concurrent runners")).toHaveValue("3")
  })

  it("shows the settings error with a retry, and still lets the steps move on", async () => {
    mockApi(
      authedRoutes({
        "GET /api/setup": { ...base, configured: true, owner: "DarkRaise" },
        "GET /api/storage": fixtures.storage,
        "GET /api/config": () => json({ error: "config unreadable" }, 500),
      }),
    )
    const { user } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Next" }))
    expect(await screen.findByText("config unreadable")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument()
    expect(screen.queryByText("loading…")).toBeNull()
    await user.click(screen.getByRole("button", { name: "Next" }))
    expect(await screen.findByRole("heading", { name: "Toolchains", level: 2 })).toBeInTheDocument()
  })

  it("leaves Web access out of the settings step", async () => {
    mockApi(authedRoutes({ "GET /api/setup": { ...base, configured: true, owner: "DarkRaise" }, "GET /api/storage": fixtures.storage }))
    const { user } = renderApp("/setup")
    expect(await screen.findByRole("heading", { name: "Repositories", level: 2 })).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Next" }))
    expect(await screen.findByLabelText("Max concurrent runners")).toBeInTheDocument()
    expect(screen.getByRole("region", { name: "Runner defaults" })).toBeInTheDocument()
    expect(screen.queryByRole("region", { name: "Web access" })).toBeNull()
    expect(screen.queryByLabelText("Listen address")).toBeNull()
  })

  it("renders without the Shell and opens the Add repository dialog", async () => {
    mockApi(
      authedRoutes({ "GET /api/setup": { ...base, configured: true }, "GET /api/repos/available": fixtures.availableRepos }),
    )
    const { user } = renderApp("/setup")
    expect(await screen.findByRole("heading", { name: "Repositories", level: 2 })).toBeInTheDocument()
    expect(screen.queryByRole("link", { name: "Dashboard" })).toBeNull()
    await user.click(screen.getByRole("button", { name: "Add repository" }))
    expect(await screen.findByRole("dialog")).toBeInTheDocument()
  })

  it.each([
    [true, "Setup finishes now and queues the popular toolchain set.", "popular"],
    [false, "Setup finishes now. No toolchain set is queued.", "none"],
  ])("confirms Skip the rest and sends what Finish would send (popular set pending: %s)", async (pending, body, toolchains) => {
    let finished: unknown
    const { calls } = mockApi(
      authedRoutes({
        "GET /api/setup": { ...base, configured: true, toolchains_pending: pending },
        "POST /api/setup/finish": ({ body }: { body: unknown }) => {
          finished = body
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Skip the rest" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText("Skip the rest of setup?")).toBeInTheDocument()
    expect(ask.getByText(body)).toBeInTheDocument()
    expect(calls.some((c) => c.path === "/api/setup/finish")).toBe(false)
    await user.click(ask.getByRole("button", { name: "Skip the rest" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
    expect(finished).toEqual({ toolchains })
  })

  it("skips without the popular set once it was switched off", async () => {
    let finished: unknown
    mockApi(
      authedRoutes({
        "GET /api/setup": { ...base, configured: true },
        "POST /api/setup/finish": ({ body }: { body: unknown }) => {
          finished = body
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Next" }))
    await screen.findByLabelText("Max concurrent runners")
    await user.click(screen.getByRole("button", { name: "Next" }))
    await user.click(await screen.findByRole("switch", { name: "Popular set" }))
    await user.click(screen.getByRole("button", { name: "Skip the rest" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText("Setup finishes now. No toolchain set is queued.")).toBeInTheDocument()
    await user.click(ask.getByRole("button", { name: "Skip the rest" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
    expect(finished).toEqual({ toolchains: "none" })
  })

  it("leaves setup open when Skip the rest is cancelled", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/setup": { ...base, configured: true }, "POST /api/setup/finish": () => noContent() }))
    const { user, router } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Skip the rest" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.path === "/api/setup/finish")).toBe(false)
    expect(router.state.location.pathname).toBe("/setup")
  })

  it("shows a busy Skip the rest that cannot be pressed again", async () => {
    const { calls } = mockApi(
      authedRoutes({ "GET /api/setup": { ...base, configured: true }, "POST /api/setup/finish": () => new Promise<Response>(() => {}) }),
    )
    const { user } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Skip the rest" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Skip the rest" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    // The kit's loading state marks the button aria-busy and keeps its label.
    const skip = screen.getByRole("button", { name: "Skip the rest" })
    expect(skip).toHaveAttribute("aria-busy", "true")
    expect(skip).toBeDisabled()
    expect(calls.filter((c) => c.path === "/api/setup/finish")).toHaveLength(1)
  })

  it("sends a visitor without a session to /login", async () => {
    mockApi({ "GET /auth/state": { setup_required: false, authenticated: false } })
    const { router } = renderApp("/setup")
    expect(await screen.findByRole("button", { name: "Log in" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/login")
  })
})
