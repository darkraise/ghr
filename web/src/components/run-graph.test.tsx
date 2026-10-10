import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"
import type { RunGraph, RunJob } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import type { JobLinkFn } from "./run-frame"
import { JobGraph } from "./run-graph"

const NOW = Date.parse("2026-10-03T14:05:00Z")
const run = fixtures.runDetail
const graph = run.graph as RunGraph

const jobLink: JobLinkFn = (jobId, children, opts = {}) => (
  <a href={`#job-${jobId}`} aria-label={opts.label} className={opts.className}>
    {children}
  </a>
)

function show(g: RunGraph = graph, jobs: RunJob[] = run.jobs) {
  const view = render(<JobGraph graph={g} jobs={jobs} path={run.path} event={run.event} now={NOW} jobLink={jobLink} />)
  return { ...view, section: within(screen.getByRole("region", { name: "ci.yml" })) }
}

describe("JobGraph", () => {
  it("is titled with the workflow file and the event", () => {
    const { section } = show()
    expect(section.getByText("pull_request")).toHaveClass("font-mono")
  })

  it("draws a focusable, scrollable region with one tile link per job", () => {
    const { section } = show()
    const region = section.getByRole("group", { name: "Job graph" })
    expect(region).toHaveAttribute("tabindex", "0")
    expect(region).toHaveClass("overflow-x-auto")
    expect(section.getByRole("link", { name: "Pick runner, Succeeded" })).toHaveAttribute("href", "#job-1001")
    expect(section.getByRole("link", { name: "Backend tests, Failed, needs Pick runner" })).toHaveAttribute("href", "#job-1002")
    expect(section.getByRole("link", { name: "Backend tests, Failed, needs Pick runner" })).toHaveTextContent("2m15s")
  })

  it("places tiles by column and keeps the lines out of the accessibility tree", () => {
    const { container } = show()
    const tile = (id: number) => (container.querySelector(`a[href="#job-${id}"]`)?.parentElement as HTMLElement).style
    expect(tile(1001).left).toBe("0px")
    expect(tile(1001).width).toBe("220px")
    expect(tile(1002).left).toBe("290px")
    const svg = container.querySelector("svg") as SVGElement
    expect(svg).toHaveAttribute("aria-hidden", "true")
    expect(svg.querySelectorAll("path")).toHaveLength(2)
  })

  it("lists a group's jobs by their full names", () => {
    const { section } = show()
    expect(section.getByText("Frontend tests (${{ matrix.shard }}/2)")).toBeInTheDocument()
    expect(section.getByRole("link", { name: "Frontend tests (1/2), Succeeded, needs Pick runner" })).toHaveTextContent("2m35s")
    expect(section.getByRole("link", { name: "Frontend tests (2/2), Succeeded, needs Pick runner" })).toBeInTheDocument()
    expect(section.getByRole("link", { name: "E2E (api), Succeeded" })).toBeInTheDocument()
  })

  it("shows a large group's first five jobs and the rest on request", async () => {
    const base = run.jobs[2] as RunJob
    const shards = Array.from({ length: 7 }, (_, i) => ({ ...base, id: 2000 + i, name: `Shard ${i + 1}` }))
    const g: RunGraph = { nodes: [{ key: "shard", name: "Shard", column: 0, needs: [], job_ids: shards.map((s) => s.id), group: true }] }
    const { section } = show(g, shards)
    expect(section.getAllByRole("link")).toHaveLength(5)
    const more = section.getByRole("button", { name: "2 more" })
    expect(more).toHaveAttribute("aria-expanded", "false")
    await userEvent.setup().click(more)
    expect(section.getAllByRole("link")).toHaveLength(7)
    expect(section.getByRole("button", { name: "Show fewer" })).toHaveAttribute("aria-expanded", "true")
  })

  it("takes the accent border on hover and keyboard focus", () => {
    const { container } = show()
    const single = container.querySelector('a[href="#job-1001"]') as HTMLElement
    expect(single).toHaveClass("hover:border-primary")
    expect(single).toHaveClass("focus-visible:border-primary")
    expect(single).not.toHaveClass("focus-within:border-primary")

    const base = run.jobs[2] as RunJob
    const shards = [0, 1].map((i) => ({ ...base, id: 2000 + i, name: `Shard ${i + 1}` }))
    const g: RunGraph = { nodes: [{ key: "shard", name: "Shard", column: 0, needs: [], job_ids: shards.map((x) => x.id), group: true }] }
    const grouped = render(<JobGraph graph={g} jobs={shards} path={run.path} event={run.event} now={NOW} jobLink={jobLink} />)
    const wrapper = grouped.container.querySelector('a[href="#job-2000"]')?.closest("div.rounded-md") as HTMLElement
    expect(wrapper).toHaveClass("hover:border-primary")
    expect(wrapper).toHaveClass("focus-within:border-primary")
  })

  it("draws the jobs on a ground canvas with thin muted connectors", () => {
    const { container } = show()
    expect(screen.getByRole("group", { name: "Job graph" })).toHaveClass("bg-background", "rounded-md", "p-3")
    const svg = container.querySelector("svg[aria-hidden='true']") as SVGElement
    expect(svg).toHaveClass("text-muted-foreground")
    expect(svg.querySelector("path")).toHaveAttribute("stroke-width", "1")
  })
})
