import { screen, within } from "@testing-library/react"
import axe from "axe-core"
import { beforeEach, describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
import { authedRoutes, fixtures, runRoutes } from "@/test/fixtures"
import { setViewport } from "@/test/media"
import { renderApp } from "@/test/render"

async function violations(root?: Element): Promise<string[]> {
  const target = root ?? document.getElementById("main-content")
  if (!target) throw new Error("no main content")
  // jsdom computes no layout or colour, so contrast is checked by the
  // palette test in src/styles/theme.test.ts instead.
  const results = await axe.run(target, { rules: { "color-contrast": { enabled: false } } })
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

describe("Actions accessibility", () => {
  it.each([[1280], [390]])(
    "has no axe violations at %ipx",
    async (width) => {
      setViewport(width)
      mockApi(authedRoutes())
      renderApp("/actions")
      await screen.findByText("old-docs: GitHub rate limit; API calls are paused. Retry after 14:30")
      await screen.findByRole("link", { name: /^Bump the runner/ })
      await screen.findByRole("link", { name: /^Fix the cache key/ })
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
      await screen.findByRole("region", { name: "Watched repositories" })
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
      expect(await violations()).toEqual([])
    },
    30_000,
  )

  it(
    "has no axe violations on the registrations section at 1280px",
    async () => {
      setViewport(1280)
      mockApi(
        authedRoutes({
          "GET /api/history": [],
          "GET /api/repos/darkmem/label-check": fixtures.labelCheck,
          "GET /api/repos/darkmem/registrations": fixtures.registrations,
        }),
      )
      renderApp("/repositories/darkmem/registrations")
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
      renderApp("/settings/toolchains")
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
      renderApp("/settings/storage")
      await screen.findByText("cleared NuGet (3.6 GB freed)")
      await screen.findByText("Last prune: manual build-cache-all at 13:16, ok, 6.8 MB freed")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})

describe("Artifacts accessibility", () => {
  it(
    "has no axe violations with every card shown",
    async () => {
      mockApi(authedRoutes())
      const { user } = renderApp("/artifacts")
      for (const name of ["Quota", "Operation", "Repositories", "Recent deletions"]) {
        await screen.findByRole("region", { name })
      }
      await user.click(screen.getByRole("button", { name: "darkmem" }))
      await screen.findByRole("region", { name: "Artifacts of darkmem" })
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
      await screen.findByRole("navigation", { name: "Settings" })
      await screen.findByLabelText("Poll interval")
      expect(await violations()).toEqual([])
    },
    30_000,
  )

  it(
    "has no axe violations on the token section at 1280px",
    async () => {
      setViewport(1280)
      mockApi(authedRoutes({ "GET /api/token": fixtures.token }))
      renderApp("/settings/token")
      await screen.findByRole("navigation", { name: "Settings" })
      await screen.findByText("4,980 of 5,000, resets 14:45")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})

describe("GitHub storage settings accessibility", () => {
  it(
    "has no axe violations at 1280px",
    async () => {
      setViewport(1280)
      mockApi(authedRoutes())
      renderApp("/settings/github-storage")
      await screen.findByRole("region", { name: "Policy" })
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})

describe("Run details accessibility", () => {
  it.each([
    [1280, "the side rail"],
    [390, "the stacked rail"],
  ])(
    "has no axe violations on the run page at %ipx (%s)",
    async (width) => {
      setViewport(width)
      mockApi(runRoutes())
      renderApp("/actions/darkmem/runs/102")
      await screen.findByRole("group", { name: "Job graph" })
      await screen.findByRole("link", { name: "Log line 5, Backend tests / Lint (ruff)" })
      expect(await violations()).toEqual([])
    },
    30_000,
  )

  it(
    "has no axe violations on the job page",
    async () => {
      mockApi(runRoutes())
      renderApp("/actions/darkmem/runs/102/jobs/1002?attempt=2&step=3&line=5")
      await screen.findByLabelText("Search logs")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})

// The kit portals dialogs out of #main-content, so they are audited by root.
describe("Dialog accessibility", () => {
  it(
    "has no axe violations in the Add repository dialog",
    async () => {
      mockApi(authedRoutes({ "GET /api/history": [], "GET /api/repos/available": fixtures.availableRepos }))
      const { user } = renderApp("/repositories")
      await user.click(await screen.findByRole("button", { name: "Add repository" }))
      const dialog = await screen.findByRole("dialog")
      await within(dialog).findByRole("option", { name: /new-repo/ })
      expect(await violations(dialog)).toEqual([])
    },
    30_000,
  )

  it(
    "has no axe violations in the Stop runner confirmation",
    async () => {
      mockApi(
        authedRoutes({
          "GET /api/runners/aaaaaa/steps": fixtures.steps,
          "GET /api/runners/aaaaaa/containers": fixtures.containers,
        }),
      )
      const { user } = renderApp("/runners/aaaaaa")
      await screen.findByText("Run tests")
      await user.click(screen.getByRole("button", { name: "Stop runner" }))
      expect(await violations(await screen.findByRole("alertdialog"))).toEqual([])
    },
    30_000,
  )

  it(
    "has no axe violations in the Unsaved changes prompt",
    async () => {
      mockApi(authedRoutes())
      const { user } = renderApp("/settings/github-storage")
      await screen.findByRole("region", { name: "Policy" })
      const included = screen.getByRole("spinbutton", { name: "Included storage" })
      await user.clear(included)
      await user.type(included, "1024")
      await user.click(screen.getAllByRole("link", { name: "Dashboard" })[0] as HTMLElement)
      expect(await violations(await screen.findByRole("alertdialog", { name: "Unsaved changes" }))).toEqual([])
    },
    30_000,
  )
})

describe("Jobs accessibility", () => {
  it.each([[1280], [390]])(
    "has no axe violations at %ipx",
    async (width) => {
      setViewport(width)
      mockApi(authedRoutes())
      renderApp("/jobs")
      await screen.findByLabelText("Prune every")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})

describe("Updates settings accessibility", () => {
  it(
    "has no axe violations at 1280px",
    async () => {
      setViewport(1280)
      mockApi(authedRoutes({ "GET /api/token": fixtures.token }))
      renderApp("/settings/updates")
      await screen.findByRole("region", { name: "Runner" })
      await screen.findByText("Up to date")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})
