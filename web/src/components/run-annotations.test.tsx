import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { RunAnnotations } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import type { JobLinkFn } from "./run-frame"
import { AnnotationsSection } from "./run-annotations"

const jobLink: JobLinkFn = (jobId, children, opts = {}) => (
  <a href={`#job-${jobId}-${opts.step}-${opts.line}`} aria-label={opts.label} className={opts.className}>
    {children}
  </a>
)

function show(data: RunAnnotations | undefined, error: unknown = null) {
  render(<AnnotationsSection data={data} error={error} onRetry={() => {}} jobLink={jobLink} />)
  return within(screen.getByRole("region", { name: "Annotations" }))
}

const none: RunAnnotations = { items: [], omitted: {}, unread: 0, expired_logs: 0, too_large: 0, failed: 0 }

describe("AnnotationsSection", () => {
  it("counts each level and lists the annotations", () => {
    const s = show(fixtures.runAnnotations)
    expect(s.getByText("2 errors")).toHaveClass("font-mono")
    expect(s.getByText("1 warning")).toBeInTheDocument()
    expect(s.getByText("1 notice")).toBeInTheDocument()
    expect(s.getByText("src/cache.py:12:5: F401 'os' imported but unused")).toHaveClass("font-mono")
    expect(s.getAllByRole("img", { name: "Error" })).toHaveLength(2)
    expect(s.getByRole("img", { name: "Warning" })).toBeInTheDocument()
    expect(s.getByRole("img", { name: "Notice" })).toBeInTheDocument()
    expect(s.getAllByText("Backend tests / Lint (ruff)")).toHaveLength(2)
    expect(s.getByText("Taken from the job logs, so file and line come only from the message text.")).toBeInTheDocument()
  })

  it("links each annotation to its log line", () => {
    const s = show(fixtures.runAnnotations)
    const link = s.getByRole("link", { name: "Log line 5, Backend tests / Lint (ruff)" })
    expect(link).toHaveTextContent("Log line 5")
    expect(link).toHaveAttribute("href", "#job-1002-3-5")
  })

  it("says how many more a job's log holds under its last row", () => {
    const s = show(fixtures.runAnnotations)
    const more = s.getByText("3 more in the log")
    const row = more.closest("li") as HTMLElement
    expect(within(row).getByText("Process completed with exit code 1.")).toBeInTheDocument()
  })

  it("shows a message without its terminal escape codes", () => {
    const s = show({ ...none, items: [{ level: "error", job_id: 1, job_name: "j", step: 1, step_name: "s", line: 1, message: "\u001b[31mF401\u001b[0m unused" }] })
    expect(s.getByText("F401 unused")).toBeInTheDocument()
  })

  it("hides the zero counts", () => {
    const s = show({ ...fixtures.runAnnotations, items: fixtures.runAnnotations.items.filter((a) => a.level === "error") })
    expect(s.getByText("2 errors")).toBeInTheDocument()
    expect(s.queryByText(/warning/)).toBeNull()
    expect(s.queryByText(/notice/)).toBeNull()
  })

  it.each([
    [{ unread: 2 }, "Reading logs for 2 jobs"],
    [{ expired_logs: 3 }, "Logs for 3 jobs have expired"],
    [{ expired_logs: 1 }, "The log for 1 job has expired"],
    [{ too_large: 2 }, "2 job logs are too large to read here"],
    [{ failed: 1 }, "1 job log could not be read"],
  ])("shows the status line for %o", (counts, line) => {
    const s = show({ ...none, ...counts })
    expect(s.getByText(line)).toBeInTheDocument()
    expect(s.queryByText("No errors, warnings or notices in the finished jobs' logs.")).toBeNull()
  })

  it("says the finished jobs' logs are clean", () => {
    const s = show(none)
    expect(s.getByText("No errors, warnings or notices in the finished jobs' logs.")).toBeInTheDocument()
  })

  it("shows the error while nothing has loaded", () => {
    const s = show(undefined, new Error("GitHub did not answer in time"))
    expect(s.getByText("GitHub did not answer in time")).toBeInTheDocument()
  })
})
