import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { fixtures } from "@/test/fixtures"
import { StatCards } from "./stat-card"

const now = Date.parse("2026-10-03T14:05:00Z")
const card = (name: string) => within(screen.getByRole("region", { name }))

describe("StatCards", () => {
  it("shows runners, waiting jobs, the API budget and the host", () => {
    render(<StatCards status={fixtures.status} metrics={fixtures.metrics} now={now} />)
    expect(card("Runners").getByText("1")).toHaveClass("font-mono")
    expect(card("Runners").getByText("of 2 busy")).toBeInTheDocument()
    expect(card("Runners").getByRole("img", { name: "1 of 2 busy, 1 warm" })).toBeInTheDocument()
    expect(card("Waiting jobs").getByText("3")).toBeInTheDocument()
    expect(card("Waiting jobs").getByText("oldest 3 min")).toBeInTheDocument()
    expect(card("Waiting jobs").getByRole("img", { name: "Waiting jobs over the last hour" })).toHaveClass("text-warning")
    expect(card("API budget").getByText("4,980")).toBeInTheDocument()
    expect(card("API budget").getByText("of 5,000")).toBeInTheDocument()
    expect(card("API budget").getByLabelText("Remaining API budget")).toHaveAttribute("aria-valuenow", "4980")
    expect(card("Host").getByText("31%")).toBeInTheDocument()
    expect(card("Host").getByText("CPU, 3.0G of 16.0G memory")).toBeInTheDocument()
    expect(card("Host").getByRole("img", { name: "CPU over the last hour" })).toBeInTheDocument()
  })

  it("reads plain busy in all mode", () => {
    render(<StatCards status={{ ...fixtures.status, mode: "all" }} metrics={fixtures.metrics} now={now} />)
    expect(card("Runners").getByText("busy")).toBeInTheDocument()
  })

  it("says the API budget is not measured until GitHub answers", () => {
    render(<StatCards status={{ ...fixtures.status, rate_limit: undefined }} metrics={fixtures.metrics} now={now} />)
    expect(card("API budget").getByText("Not measured yet")).toBeInTheDocument()
    expect(card("API budget").queryByLabelText("Remaining API budget")).toBeNull()
  })

  it("waits for host metrics", () => {
    render(<StatCards status={fixtures.status} metrics={undefined} now={now} />)
    expect(card("Host").getByText("Not measured yet")).toBeInTheDocument()
  })

  it("leaves out the oldest age when nothing waits", () => {
    const repos = fixtures.status.repos.map((r) => ({ ...r, queued: 0, oldest_queued_at: undefined }))
    render(<StatCards status={{ ...fixtures.status, repos }} metrics={fixtures.metrics} now={now} />)
    expect(card("Waiting jobs").queryByText(/oldest/)).toBeNull()
  })
})
