import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { RouterAdapterProvider, type RouterAdapter } from "darkraise-ui/router"
import { describe, expect, it, vi } from "vitest"
import type { RunDetail } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { RunFrame, type JobLinkFn, type SummaryLinkFn } from "./run-frame"

// PageHeader's breadcrumbs link through the kit's router adapter; plain
// anchors stand in for the router here.
const adapter: RouterAdapter = {
  Link: ({ to, className, children }) => (
    <a href={to} className={className}>
      {children}
    </a>
  ),
  useNavigate: () => () => {},
  usePathname: () => "/",
  useBack: () => () => {},
  useInvalidate: () => () => {},
}

const jobLink: JobLinkFn = (jobId, children, opts) => (
  <a href={`#job-${jobId}`} className={opts?.className} aria-label={opts?.label} aria-current={opts?.current ? "page" : undefined}>
    {children}
  </a>
)
const summaryLink: SummaryLinkFn = (children, opts) => (
  <a href="#summary" className={opts.className} aria-current={opts.current ? "page" : undefined}>
    {children}
  </a>
)

const now = Date.parse("2026-10-03T14:05:00Z")

function renderFrame(over: Partial<Parameters<typeof RunFrame>[0]> = {}, run: RunDetail = fixtures.runDetail) {
  const onAttempt = vi.fn()
  const user = userEvent.setup()
  render(
    <RouterAdapterProvider value={adapter}>
      <RunFrame run={run} owner="darkraise" now={now} current="summary" jobLink={jobLink} summaryLink={summaryLink} onAttempt={onAttempt} {...over}>
        <p>Run content</p>
      </RunFrame>
    </RouterAdapterProvider>,
  )
  return { onAttempt, user }
}

describe("RunFrame", () => {
  it("heads the page with the run title, its description and breadcrumbs", () => {
    renderFrame()
    expect(screen.getByRole("heading", { name: "Fix the cache key" })).toBeInTheDocument()
    expect(screen.getByText("ci #42 in darkmem on fix/cache-key.")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Actions" })).toHaveAttribute("href", "/actions")
    expect(screen.getByRole("link", { name: "darkmem" })).toHaveAttribute("href", "/actions?repo=darkmem")
    expect(screen.getByText("ci")).toBeInTheDocument()
    expect(screen.getByText("#42")).not.toHaveAttribute("href")
    expect(screen.getByText("Run content")).toBeInTheDocument()
  })

  it("adds the job to the breadcrumbs and links the run number on a job page", () => {
    renderFrame({ job: fixtures.runDetail.jobs[1], current: 1002 })
    expect(screen.getByRole("link", { name: "#42" })).toHaveAttribute("href", "/actions/darkmem/runs/102?attempt=2")
    expect(screen.getAllByText("Backend tests").length).toBeGreaterThan(1)
  })

  it("opens the run on GitHub in a new tab", () => {
    renderFrame()
    const link = screen.getByRole("link", { name: "Open on GitHub" })
    expect(link).toHaveAttribute("href", "https://github.com/darkraise/darkmem/actions/runs/102")
    expect(link).toHaveAttribute("target", "_blank")
  })

  it("picks an attempt, latest first", async () => {
    const { onAttempt, user } = renderFrame()
    const picker = screen.getByRole("combobox", { name: "Attempt" })
    expect(picker).toHaveTextContent("Attempt 2 (latest)")
    await user.click(picker)
    expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual(["Attempt 2 (latest)", "Attempt 1"])
    await user.click(screen.getByRole("option", { name: "Attempt 1" }))
    expect(onAttempt).toHaveBeenCalledWith(1)
  })

  it("hides the attempt picker for a run with one attempt", () => {
    renderFrame({}, { ...fixtures.runDetail, attempt: 1, attempts: 1 })
    expect(screen.queryByRole("combobox", { name: "Attempt" })).toBeNull()
  })

  it("lists the summary, every job with its status and duration, and the workflow file in the rail", () => {
    renderFrame({ current: 1002 })
    const rail = within(screen.getByRole("navigation", { name: "Run" }))
    expect(rail.getByRole("link", { name: "Summary" })).not.toHaveAttribute("aria-current")
    const links = rail.getAllByRole("link").filter((a) => a.getAttribute("href")?.startsWith("#job-"))
    expect(links.map((a) => a.getAttribute("href"))).toEqual(["#job-1001", "#job-1002", "#job-1003", "#job-1004", "#job-1005"])
    const backend = within(rail.getByRole("link", { name: /Backend tests/ }))
    expect(backend.getByRole("img", { name: "Failed" })).toBeInTheDocument()
    expect(backend.getByText("2m15s")).toHaveClass("font-mono")
    expect(rail.getByRole("link", { name: /Backend tests/ })).toHaveAttribute("aria-current", "page")
    expect(rail.getByText("Jobs")).toBeInTheDocument()
    expect(rail.getByText("Run details")).toBeInTheDocument()
    expect(rail.getByRole("link", { name: /Workflow file/ })).toHaveAttribute(
      "href",
      "https://github.com/darkraise/darkmem/blob/a1f9c3e0d4b5c6a7b8c9d0e1f2a3b4c5d6e7f8a9/.github/workflows/ci.yml",
    )
  })

  it("marks the summary as current on the run page", () => {
    renderFrame()
    const rail = within(screen.getByRole("navigation", { name: "Run" }))
    expect(rail.getByRole("link", { name: "Summary" })).toHaveAttribute("aria-current", "page")
  })

  it("leaves out the workflow file link for a run without a workflow file", () => {
    renderFrame({}, { ...fixtures.runDetail, path: "dynamic/dependabot/dependabot-updates" })
    expect(screen.queryByRole("link", { name: /Workflow file/ })).toBeNull()
    expect(screen.queryByText("Run details")).toBeNull()
  })

  it("stacks the rail above the content below 800 px", () => {
    renderFrame()
    const rail = screen.getByRole("navigation", { name: "Run" })
    expect(rail).toHaveClass("min-[800px]:sticky")
    expect(rail.parentElement).toHaveClass("min-[800px]:grid-cols-[15rem_minmax(0,1fr)]")
  })
})
