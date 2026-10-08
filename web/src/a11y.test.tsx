import { screen } from "@testing-library/react"
import axe from "axe-core"
import { beforeEach, describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
import { authedRoutes } from "@/test/fixtures"
import { renderApp } from "@/test/render"

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
      const main = document.getElementById("main-content")
      if (!main) throw new Error("no main content")
      // jsdom computes no layout or colour, so contrast is checked by the
      // palette test in src/styles/theme.test.ts instead.
      const results = await axe.run(main, { rules: { "color-contrast": { enabled: false } } })
      expect(results.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([])
    },
    30_000,
  )
})
