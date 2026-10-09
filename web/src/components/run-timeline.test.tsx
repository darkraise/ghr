import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { RunDetail } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import type { JobLinkFn } from "./run-frame"
import { JobsTimeline } from "./run-timeline"

const NOW = Date.parse("2026-10-03T14:05:00Z")

const jobLink: JobLinkFn = (jobId, children, opts = {}) => (
  <a href={`#job-${jobId}`} className={opts.className}>
    {children}
  </a>
)

function show(over: Partial<RunDetail> = {}) {
  const run: RunDetail = { ...fixtures.runDetail, graph: undefined, graph_note: "This run has no workflow file to read.", ...over }
  const view = render(<JobsTimeline run={run} now={NOW} jobLink={jobLink} />)
  return { ...view, section: within(screen.getByRole("region", { name: "Jobs timeline" })) }
}

describe("JobsTimeline", () => {
  it.each([
    "Add Contents: read to the token to see which jobs depend on which.",
    "This run has no workflow file to read.",
    "The workflow file at this commit does not match the run's jobs.",
    "ghr could not read .github/workflows/ci.yml.",
  ])("says why there is no graph: %s", (note) => {
    const { section } = show({ graph_note: note })
    expect(section.getByText(note)).toBeInTheDocument()
  })

  it("lists the jobs by start with their durations as text", () => {
    const { section } = show()
    const links = section.getAllByRole("link")
    expect(links.map((l) => l.textContent)).toEqual([
      "Pick runner",
      "Backend tests",
      "Frontend tests (1/2)",
      "Frontend tests (2/2)",
      "E2E (api)",
    ])
    expect(links[1]).toHaveAttribute("href", "#job-1002")
    const row = within(links[1]?.closest("li") as HTMLElement)
    expect(row.getByText("2m15s")).toHaveClass("font-mono")
  })

  it("draws each bar against minutes since the run started", () => {
    const { container } = show()
    const ticks = container.querySelector('[aria-hidden="true"].font-mono') as HTMLElement
    expect(ticks.textContent).toBe("0m1m2m3m4m")
    const bars = container.querySelectorAll<HTMLElement>("[data-bar]")
    expect(bars).toHaveLength(5)
    expect(bars[0]?.style.left).toBe("0%")
  })

  it("spaces the axis labels out for a long run", () => {
    const { container } = show({ status: "in_progress", started_at: "2026-10-03T11:00:00Z" })
    const ticks = container.querySelector('[aria-hidden="true"].font-mono') as HTMLElement
    expect(ticks.children.length).toBeLessThanOrEqual(12)
    expect(ticks.children[0]).toHaveTextContent("0m")
  })

  it("says skipped or queued for a job that never started", () => {
    const [first, second, ...rest] = fixtures.runDetail.jobs
    if (!first || !second) throw new Error("fixture has too few jobs")
    const { section } = show({
      jobs: [
        { ...first, status: "completed", conclusion: "skipped", started_at: undefined, completed_at: undefined },
        { ...second, status: "queued", conclusion: "", started_at: undefined, completed_at: undefined },
        ...rest,
      ],
    })
    expect(section.getByText("skipped")).toBeInTheDocument()
    expect(section.getByText("queued")).toBeInTheDocument()
    expect(section.getAllByRole("link").at(-1)).toHaveTextContent("Backend tests")
  })
})
