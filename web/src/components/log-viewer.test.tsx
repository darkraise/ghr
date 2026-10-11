import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"
import type { JobLog, LogLine, Step } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { LogViewer } from "./log-viewer"

const steps: Step[] = fixtures.runDetail.jobs[1]?.steps ?? []

function renderViewer(props: Partial<Parameters<typeof LogViewer>[0]> = {}) {
  const user = userEvent.setup()
  const view = render(<LogViewer log={fixtures.jobLog} steps={steps} {...props} />)
  return { ...view, user }
}

const stepButton = (name: RegExp) => screen.getByRole("button", { name })
const lineRow = (n: number, step: string) => {
  const item = screen.getByRole("button", { name: new RegExp(step) }).closest("li") as HTMLElement
  return item.querySelector(`[data-line="${n}"]`) as HTMLElement | null
}

function bigLog(count: number, special: Record<number, string> = {}): JobLog {
  const lines: LogLine[] = Array.from({ length: count }, (_, i) => ({ n: i + 1, kind: "", text: special[i + 1] ?? `row ${i + 1}` }))
  return { job_id: 9, too_large: false, expired: false, no_log: false, steps: [{ number: 1, name: "Build", lines }] }
}
const bigSteps: Step[] = [{ number: 1, name: "Build", status: "completed", conclusion: "failure" }]

describe("LogViewer", () => {
  it("holds the heading, the search box, the toolbar and the steps in one dark panel", () => {
    renderViewer({ heading: <span>lint 1m12s</span>, toolbar: <button type="button">Raw log</button> })
    const panel = screen.getByRole("list", { name: "Steps" }).closest(".ghr-log-panel") as HTMLElement
    expect(panel).not.toBeNull()
    expect(within(panel).getByText("lint 1m12s")).toBeInTheDocument()
    expect(within(panel).getByLabelText("Search logs")).toBeInTheDocument()
    expect(within(panel).getByRole("button", { name: "Raw log" })).toBeInTheDocument()
  })

  it("lists every step with its status and duration, the failed one open", () => {
    renderViewer()
    expect(screen.getAllByRole("button", { expanded: false }).length).toBeGreaterThan(0)
    const lint = stepButton(/Lint \(ruff\)/)
    expect(lint).toHaveAttribute("aria-expanded", "true")
    expect(within(lint).getByRole("img", { name: "Failed" })).toBeInTheDocument()
    expect(within(lint).getByText("1m25s")).toHaveClass("font-mono")
    expect(stepButton(/Set up job/)).toHaveAttribute("aria-expanded", "false")
    expect(within(stepButton(/Test$/)).getByRole("img", { name: "Skipped" })).toBeInTheDocument()
    expect(screen.queryByText("Current runner version: '2.338.0'")).toBeNull()
  })

  it("numbers lines per step and styles each kind", () => {
    const { container } = renderViewer()
    const error = lineRow(5, "Lint")
    expect(error).toHaveClass("ghr-log-error")
    expect(error).toHaveTextContent("5Error: src/cache.py:12:5: F401 'os' imported but unused")
    expect(error?.querySelector("span")).toHaveClass("select-none", "ghr-log-muted")
    expect(lineRow(6, "Lint")?.querySelector(".ghr-log-muted.min-w-0")).toHaveTextContent("Finishing: Lint (ruff)")
    expect(lineRow(3, "Lint")).toBeNull()
    const ansi = lineRow(4, "Lint")?.querySelector(".ghr-log-ansi-red")
    expect(ansi).toHaveTextContent("Found 1 error.")
    expect(container.querySelector(".ghr-log")).not.toBeNull()
  })

  it("shows commands in the primary colour and opens steps on click", async () => {
    const { user } = renderViewer()
    await user.click(stepButton(/Install/))
    expect(stepButton(/Install/)).toHaveAttribute("aria-expanded", "true")
    expect(lineRow(1, "Install")?.querySelector(".ghr-log-command")).toHaveTextContent("pip install -r requirements.txt")
    await user.click(stepButton(/Install/))
    expect(lineRow(1, "Install")).toBeNull()
  })

  it("folds groups until opened", async () => {
    const { user } = renderViewer()
    const group = screen.getByRole("button", { name: "Run ruff check ." })
    expect(group).toHaveAttribute("aria-expanded", "false")
    expect(screen.queryByText("ruff check .")).toBeNull()
    await user.click(group)
    expect(group).toHaveAttribute("aria-expanded", "true")
    expect(screen.getByText("ruff check .")).toBeInTheDocument()
  })

  it("prefixes notices without a tint and warnings with one", () => {
    const log: JobLog = {
      job_id: 9,
      too_large: false,
      expired: false,
      no_log: false,
      steps: [
        {
          number: 1,
          name: "Build",
          lines: [
            { n: 1, kind: "notice", text: "Retried 1 flaky test" },
            { n: 2, kind: "warning", text: "Node.js 16 actions are deprecated." },
          ],
        },
      ],
    }
    const { container } = render(<LogViewer log={log} steps={bigSteps} />)
    const notice = container.querySelector('[data-line="1"]')
    expect(notice).toHaveTextContent("Notice: Retried 1 flaky test")
    expect(notice?.className).not.toMatch(/ghr-log-(error|warning)/)
    expect(container.querySelector('[data-line="2"]')).toHaveClass("ghr-log-warning")
    expect(container.querySelector('[data-line="2"]')).toHaveTextContent("Warning: Node.js 16 actions are deprecated.")
  })

  it("opens the step and group of the line in the URL and marks the line", () => {
    renderViewer({ step: 3, line: 2 })
    expect(screen.getByRole("button", { name: "Run ruff check ." })).toHaveAttribute("aria-expanded", "true")
    const row = lineRow(2, "Lint")
    expect(row).toHaveAttribute("data-target", "true")
    expect(row).toHaveClass("ghr-log-target")
  })

  it("opens the step in the URL instead of the failed one", () => {
    renderViewer({ step: 1 })
    expect(stepButton(/Set up job/)).toHaveAttribute("aria-expanded", "true")
    expect(stepButton(/Lint \(ruff\)/)).toHaveAttribute("aria-expanded", "false")
  })

  it("shows the toolbar after the search box", () => {
    renderViewer({ toolbar: <button type="button">Raw log</button> })
    expect(screen.getByRole("searchbox", { name: "Search logs" })).toHaveAttribute("placeholder", "Search logs")
    expect(screen.getByRole("button", { name: "Raw log" })).toBeInTheDocument()
  })

  it("counts matches and opens only the current match's step and group", async () => {
    const { user } = renderViewer()
    const box = screen.getByRole("searchbox", { name: "Search logs" })
    await user.type(box, "RUFF")
    expect(screen.getByText("1 of 4")).toBeInTheDocument()
    expect(stepButton(/Install/)).toHaveAttribute("aria-expanded", "true")
    expect(stepButton(/Set up job/)).toHaveAttribute("aria-expanded", "false")
    const first = lineRow(2, "Install")?.querySelector("mark")
    expect(first).toHaveAttribute("data-current", "true")
    expect(first).toHaveTextContent("ruff")

    await user.keyboard("{Enter}")
    expect(screen.getByText("2 of 4")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Run ruff check ." }).querySelector("mark")).toHaveAttribute("data-current", "true")
    expect(screen.getByRole("button", { name: "Run ruff check ." })).toHaveAttribute("aria-expanded", "false")

    await user.keyboard("{Enter}")
    expect(screen.getByText("3 of 4")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Run ruff check ." })).toHaveAttribute("aria-expanded", "true")
    expect(lineRow(2, "Lint")?.querySelector("mark")).toHaveAttribute("data-current", "true")
    expect(lineRow(6, "Lint")?.querySelector("mark")).not.toHaveAttribute("data-current")

    await user.keyboard("{Shift>}{Enter}{/Shift}")
    expect(screen.getByText("2 of 4")).toBeInTheDocument()
    await user.keyboard("{Shift>}{Enter}{/Shift}{Shift>}{Enter}{/Shift}")
    expect(screen.getByText("4 of 4")).toBeInTheDocument()
  })

  it("says when nothing matches", async () => {
    const { user } = renderViewer()
    expect(screen.queryByText(/ of /)).toBeNull()
    await user.type(screen.getByRole("searchbox", { name: "Search logs" }), "nothing like this")
    expect(screen.getByText("0 of 0")).toBeInTheDocument()
  })

  it("renders the last 5,000 lines of a long step until asked for more", { timeout: 20_000 }, async () => {
    const user = userEvent.setup()
    const { container } = render(<LogViewer log={bigLog(6000)} steps={bigSteps} />)
    expect(container.querySelector('[data-line="1000"]')).toBeNull()
    expect(container.querySelector('[data-line="1001"]')).not.toBeNull()
    await user.click(screen.getByRole("button", { name: "Show 1000 earlier lines" }))
    expect(container.querySelector('[data-line="1"]')).not.toBeNull()
    expect(screen.queryByRole("button", { name: /earlier lines/ })).toBeNull()
  })

  it("keeps a group that straddles the cut folded under its header", { timeout: 20_000 }, () => {
    const lines: LogLine[] = Array.from({ length: 6000 }, (_, i) => ({ n: i + 1, kind: "", text: `row ${i + 1}` }))
    lines[899] = { n: 900, kind: "group", text: "Straddling group" }
    const log: JobLog = { job_id: 9, too_large: false, expired: false, no_log: false, steps: [{ number: 1, name: "Build", lines }] }
    const { container } = render(<LogViewer log={log} steps={bigSteps} />)
    expect(screen.getByRole("button", { name: "Straddling group" })).toHaveAttribute("aria-expanded", "false")
    expect(container.querySelector('[data-line="1001"]')).toBeNull()
  })

  it("renders back to the URL's line when it is above the cut", { timeout: 20_000 }, () => {
    const { container } = render(<LogViewer log={bigLog(6000)} steps={bigSteps} step={1} line={10} />)
    expect(container.querySelector('[data-line="10"]')).toHaveAttribute("data-target", "true")
    expect(container.querySelector('[data-line="9"]')).toBeNull()
    expect(screen.getByRole("button", { name: "Show 9 earlier lines" })).toBeInTheDocument()
  })

  it("renders back to the current match when it is above the cut", { timeout: 20_000 }, async () => {
    const user = userEvent.setup()
    const { container } = render(<LogViewer log={bigLog(6000, { 3: "the needle is here" })} steps={bigSteps} />)
    expect(container.querySelector('[data-line="3"]')).toBeNull()
    await user.type(screen.getByRole("searchbox", { name: "Search logs" }), "needle")
    expect(screen.getByText("1 of 1")).toBeInTheDocument()
    expect(container.querySelector('[data-line="3"] mark')).toHaveAttribute("data-current", "true")
  })
})
