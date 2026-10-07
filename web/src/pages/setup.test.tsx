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

    const max = await screen.findByLabelText("Global max")
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

  it("renders without the Shell and opens the Add repository dialog", async () => {
    mockApi(
      authedRoutes({ "GET /api/setup": { ...base, configured: true }, "GET /api/repos/available": fixtures.availableRepos }),
    )
    const { user } = renderApp("/setup")
    expect(await screen.findByRole("heading", { name: "Repositories", level: 2 })).toBeInTheDocument()
    expect(screen.queryByRole("link", { name: "Dashboard" })).toBeNull()
    await user.click(screen.getByRole("button", { name: "+ Add repository" }))
    expect(await screen.findByRole("dialog")).toBeInTheDocument()
  })

  it("finishes without toolchains when the rest is skipped", async () => {
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
    await user.click(await screen.findByRole("button", { name: "Skip the rest" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
    expect(finished).toEqual({ toolchains: "none" })
  })

  it("sends a visitor without a session to /login", async () => {
    mockApi({ "GET /auth/state": { setup_required: false, authenticated: false } })
    const { router } = renderApp("/setup")
    expect(await screen.findByRole("button", { name: "Log in" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/login")
  })
})
