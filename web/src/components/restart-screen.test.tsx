import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, onTestFinished, vi } from "vitest"
import { keys } from "@/api/hooks"
import { liveConfig, setLive } from "@/api/live"
import { json, mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"
import { RestartScreen } from "./restart-screen"

function stubLocation() {
  const assign = vi.fn()
  const reload = vi.fn()
  const { href, origin, protocol, host, hostname, port, pathname, search, hash } = window.location
  vi.stubGlobal("location", { href, origin, protocol, host, hostname, port, pathname, search, hash, assign, reload })
  return { assign, reload }
}

async function tick(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

const down = () => {
  throw new TypeError("Failed to fetch")
}

const oldProcess = () => ({ setup_required: false, authenticated: true })
const failedStatus = (failed?: string) => ({ ...fixtures.status, restart: { available: true, failed } })

describe("RestartScreen", () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it("asks ghr every second on the page's origin until the new process answers logged out", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] })
    // Down, then the old process with this session, then the new one.
    const answers = [down, oldProcess, () => ({ setup_required: false, authenticated: false })]
    let polls = 0
    const { calls } = mockApi({
      "GET /auth/state": () => (answers[polls++] ?? down)(),
      "GET /api/status": failedStatus(),
    })
    const { assign } = stubLocation()
    render(<RestartScreen url="http://localhost" same />)
    expect(screen.getByText("Restarting ghr…")).toBeInTheDocument()
    await tick(999)
    expect(calls).toHaveLength(0)
    await tick(1)
    expect(calls.map((c) => c.path)).toEqual(["/auth/state"])
    await tick(1000)
    await vi.waitFor(() => expect(calls.map((c) => c.path)).toEqual(["/auth/state", "/auth/state", "/api/status"]))
    expect(assign).not.toHaveBeenCalled()
    await tick(1000)
    await vi.waitFor(() => expect(assign).toHaveBeenCalledWith("http://localhost/login"))
    expect(calls).toHaveLength(4)
  })

  it("says why an accepted restart did not happen, stops asking, and reloads on request", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] })
    const { calls } = mockApi({
      "GET /auth/state": oldProcess,
      "GET /api/status": failedStatus("installing node 24 started"),
    })
    const { assign, reload } = stubLocation()
    render(<RestartScreen url="http://localhost" same />)
    await tick(1000)
    await vi.waitFor(() => expect(screen.getByText("ghr did not restart: installing node 24 started")).toBeInTheDocument())
    expect(screen.queryByText("Restarting ghr…")).toBeNull()
    await tick(60_000)
    expect(calls).toHaveLength(2)
    expect(screen.queryByText("ghr has not answered yet.")).toBeNull()
    expect(assign).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Back to ghr" }))
    expect(reload).toHaveBeenCalled()
  })

  it("does not load another origin when the restart did not happen", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] })
    mockApi({
      "GET /auth/state": oldProcess,
      "GET /api/status": failedStatus("systemctl restart: exit status 5: Unit ghr.service not loaded."),
    })
    const { assign } = stubLocation()
    render(<RestartScreen url="http://192.168.0.99:9090" same={false} />)
    await tick(1000)
    await vi.waitFor(() =>
      expect(screen.getByText("ghr did not restart: systemctl restart: exit status 5: Unit ghr.service not loaded.")).toBeInTheDocument(),
    )
    await tick(20_000)
    expect(assign).not.toHaveBeenCalled()
  })

  it("waits 10 seconds before loading another origin, which it cannot ask, asking only its own meanwhile", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] })
    const { calls } = mockApi({ "GET /auth/state": oldProcess, "GET /api/status": failedStatus() })
    const { assign } = stubLocation()
    render(<RestartScreen url="http://192.168.0.99:9090" same={false} />)
    await tick(9_999)
    expect(assign).not.toHaveBeenCalled()
    await tick(1)
    expect(assign).toHaveBeenCalledWith("http://192.168.0.99:9090/login")
    expect(new Set(calls.map((c) => c.path))).toEqual(new Set(["/auth/state", "/api/status"]))
    const asked = calls.length
    await tick(5000)
    expect(calls).toHaveLength(asked)
  })

  it("promises nothing after loading another origin", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] })
    mockApi({})
    stubLocation()
    render(<RestartScreen url="http://192.168.0.99:9090" same={false} />)
    await tick(120_000)
    expect(screen.queryByText("ghr has not answered yet.")).toBeNull()
    expect(screen.queryByRole("link")).toBeNull()
  })

  it("keeps asking its own origin, and says so with a link after 60 seconds", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] })
    const { calls } = mockApi({ "GET /auth/state": down })
    const { assign } = stubLocation()
    render(<RestartScreen url="http://localhost" same />)
    await tick(59_999)
    expect(screen.queryByText("ghr has not answered yet.")).toBeNull()
    await tick(1)
    expect(screen.getByText("ghr has not answered yet.")).toBeInTheDocument()
    expect(screen.getAllByRole("link").map((a) => a.getAttribute("href"))).toEqual(["http://localhost"])
    expect(calls.length).toBeGreaterThan(30)
    expect(assign).not.toHaveBeenCalled()
  })

  it("stops asking once it is gone", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] })
    const { calls } = mockApi({ "GET /auth/state": down })
    stubLocation()
    const { unmount } = render(<RestartScreen url="http://localhost" same />)
    await tick(1000)
    unmount()
    await tick(5000)
    expect(calls).toHaveLength(1)
  })
})

// An open change stream the test can greet, built as in use-change-stream.test.tsx.
function openStream() {
  let ctrl: ReadableStreamDefaultController<Uint8Array> | undefined
  const response = new Response(
    new ReadableStream<Uint8Array>({
      start(c) {
        ctrl = c
      },
    }),
    { status: 200 },
  )
  return { response, hello: () => ctrl?.enqueue(new TextEncoder().encode("event: hello\ndata: {}\n\n")) }
}

describe("restarting from the app", () => {
  it("replaces every page, closes the change stream, and keeps the screen when a late request meets a 401", async () => {
    liveConfig.enabled = true
    onTestFinished(() => {
      liveConfig.enabled = false
      setLive(false)
    })
    let restarted = false
    let holding = false
    let answerHeld: () => void = () => {}
    const held = new Promise<void>((resolve) => {
      answerHeld = resolve
    })
    const stream = openStream()
    let streams = 0
    const status = { ...fixtures.status, restart: { pending: ["web.hosts"], available: true } }
    const unauthorized = () => json({ error: "not logged in" }, 401)
    const after = (value: unknown) => () => (restarted ? unauthorized() : value)
    const { calls, fetchMock } = mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated: !restarted }),
        "GET /api/changes": () => (streams++ === 0 ? stream.response : json({ error: "gone" }, 503)),
        "GET /api/status": async () => {
          if (!holding) return restarted ? unauthorized() : status
          await held
          return unauthorized()
        },
        "GET /api/config": after(fixtures.config),
        "GET /api/metrics": after(fixtures.metrics),
        "GET /api/events": after(fixtures.events),
        "POST /api/restart": () => {
          restarted = true
          return new Response(null, { status: 202 })
        },
      }),
    )
    const { user, router, queryClient } = renderApp("/")
    const button = await screen.findByRole("button", { name: "Restart ghr" })
    await waitFor(() => expect(calls.some((c) => c.path === "/api/changes")).toBe(true))
    stream.hello()

    // A status read still in flight when the restart is accepted.
    holding = true
    const statusReads = calls.filter((c) => c.path === "/api/status").length
    void queryClient.refetchQueries({ queryKey: keys.status })
    await waitFor(() => expect(calls.filter((c) => c.path === "/api/status")).toHaveLength(statusReads + 1))

    const { assign } = stubLocation()
    await waitFor(() => expect(button).toBeEnabled())
    await user.click(button)
    const dialog = await screen.findByRole("alertdialog", { name: "Restart ghr?" })
    await user.click(within(dialog).getByRole("button", { name: "Restart" }))
    expect(await screen.findByText("Restarting ghr…")).toBeInTheDocument()
    const shown = calls.length
    expect(screen.queryByRole("navigation", { name: "Pages" })).toBeNull()
    expect(queryClient.getQueryCache().getAll()).toEqual([])
    const streamCall = fetchMock.mock.calls.find(([input]) => String(input) === "/api/changes")
    expect(streamCall?.[1]?.signal?.aborted).toBe(true)

    answerHeld()
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(screen.getByText("Restarting ghr…")).toBeInTheDocument()
    expect(screen.queryByText("Log in to manage this runner host.")).toBeNull()

    await waitFor(() => expect(assign).toHaveBeenCalledWith("http://localhost/login"), { timeout: 3000 })
    expect(screen.getByText("Restarting ghr…")).toBeInTheDocument()
    expect(calls.slice(shown).filter((c) => c.path.startsWith("/api/"))).toEqual([])
    expect(calls.filter((c) => c.path === "/api/changes")).toHaveLength(1)
  })
})
