import { screen } from "@testing-library/react"
import axe from "axe-core"
import { beforeEach, describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { setViewport } from "@/test/media"
import { renderApp } from "@/test/render"

async function violations(): Promise<string[]> {
  const main = document.getElementById("main-content")
  if (!main) throw new Error("no main content")
  // jsdom computes no layout or colour, so contrast is checked by the
  // palette test in src/styles/theme.test.ts instead.
  const results = await axe.run(main, { rules: { "color-contrast": { enabled: false } } })
  return results.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)
}

describe("Dashboard accessibility", () => {
  beforeEach(() => localStorage.clear())

  it.each([
    ["1h", "Runner lanes for the last hour"],
    ["24h", "Activity per bucket for the last 24 hours"],
  ])(
    "has no axe violations with the %s window",
    async (win, chart) => {
      localStorage.setItem("ghr-activity-window", win)
      mockApi(authedRoutes())
      renderApp("/")
      await screen.findByRole("group", { name: chart })
      await screen.findByText("146.0 GB of 240.0 GB used, prunes at 80%")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})

describe("Runners accessibility", () => {
  it.each([
    [1280, "the split view"],
    [390, "the narrow panel"],
  ])(
    "has no axe violations at %ipx (%s)",
    async (width) => {
      setViewport(width)
      mockApi(
        authedRoutes({
          "GET /api/runners/aaaaaa/steps": fixtures.steps,
          "GET /api/runners/aaaaaa/containers": fixtures.containers,
        }),
      )
      renderApp("/runners/aaaaaa")
      await screen.findByRole("heading", { name: "aaaaaa", level: 2 })
      await screen.findByText("Run tests")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})

describe("History accessibility", () => {
  it(
    "has no axe violations with a bucket picked",
    async () => {
      mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
      const { user } = renderApp("/history")
      await screen.findByText("#7")
      await user.click(await screen.findByRole("button", { name: /^13:00 to 14:00/ }))
      await screen.findByText("Showing 13:00 to 14:00, Oct 3")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})

describe("Repositories accessibility", () => {
  it(
    "has no axe violations",
    async () => {
      mockApi(authedRoutes())
      renderApp("/repositories")
      await screen.findByRole("region", { name: "Configured repositories" })
      await screen.findByText("50%")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})

describe("Repository page accessibility", () => {
  it(
    "has no axe violations at 1280px",
    async () => {
      setViewport(1280)
      mockApi(
        authedRoutes({
          "GET /api/history": [],
          "GET /api/repos/darkmem/label-check": fixtures.labelCheck,
          "GET /api/repos/darkmem/registrations": fixtures.registrations,
        }),
      )
      renderApp("/repositories/darkmem")
      await screen.findByRole("group", { name: "Jobs per bucket for the last 24 hours" })
      await screen.findByText("ghr-aaaaaa")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})

describe("Toolchains accessibility", () => {
  it(
    "has no axe violations",
    async () => {
      mockApi(authedRoutes())
      renderApp("/toolchains")
      await screen.findByRole("img", { name: /^Tool cache: / })
      await screen.findByText("Installing node 24, extracting, 1 more queued")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})

describe("Storage accessibility", () => {
  it(
    "has no axe violations",
    async () => {
      mockApi(authedRoutes())
      renderApp("/storage")
      await screen.findByText("cleared NuGet (3.6 GB freed)")
      await screen.findByText("Last prune: manual build-cache-all at 13:16, ok")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})

describe("Settings accessibility", () => {
  it(
    "has no axe violations at 1280px",
    async () => {
      setViewport(1280)
      mockApi(authedRoutes({ "GET /api/token": fixtures.token }))
      renderApp("/settings")
      await screen.findByRole("navigation", { name: "Sections" })
      await screen.findByText("4,980 of 5,000, resets 14:45")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})
