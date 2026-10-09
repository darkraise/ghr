import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import type { ComponentProps } from "react"
import { describe, expect, it, vi } from "vitest"
import { fixtures } from "@/test/fixtures"
import { ActivityPanel } from "./activity-panel"

const now = Date.parse("2026-10-03T14:05:00Z")

function draw(over: Partial<ComponentProps<typeof ActivityPanel>> = {}) {
  const props: ComponentProps<typeof ActivityPanel> = {
    selected: "1h",
    onSelect: vi.fn(),
    activity: fixtures.activityLanes,
    error: null,
    onRetry: vi.fn(),
    now,
    onOpenRunner: vi.fn(),
    ...over,
  }
  render(<ActivityPanel {...props} />)
  return props
}

const legend = () => within(screen.getByRole("list", { name: "Legend" }))

describe("ActivityPanel", () => {
  it("shows the range, a lanes legend and the lanes for 1h", () => {
    draw()
    expect(screen.getByRole("heading", { name: "Activity" })).toBeInTheDocument()
    expect(screen.getByText("13:05 to 14:05")).toBeInTheDocument()
    for (const key of ["Succeeded", "Failed", "Unknown", "Running", "Warm", "Starting", "Jobs waiting", "Memory"]) {
      expect(legend().getByText(key)).toBeInTheDocument()
    }
    expect(screen.getByRole("group", { name: "Runner lanes for the last hour" })).toBeInTheDocument()
  })

  it("shows buckets with their own legend", () => {
    draw({ selected: "24h", activity: fixtures.activityBuckets })
    expect(screen.getByText("Oct 2 14:00 to Oct 3 14:05, per hour")).toBeInTheDocument()
    expect(legend().getByText("Busy runner-minutes")).toBeInTheDocument()
    expect(screen.getByRole("group", { name: "Activity per bucket for the last 24 hours" })).toBeInTheDocument()
  })

  it("calls busy time slot time when there is a capacity", () => {
    draw({ selected: "24h", activity: { ...fixtures.activityBuckets, capacity: 2 } })
    expect(legend().getByText("Busy slot time")).toBeInTheDocument()
  })

  it("reports a window choice", async () => {
    const props = draw()
    await userEvent.setup().click(screen.getByRole("radio", { name: "7d" }))
    expect(props.onSelect).toHaveBeenCalledWith("7d")
  })

  it("sums up the window for screen readers, with a table of the same data", () => {
    draw()
    expect(screen.getByText("3 jobs in the last hour, 1 failed, 1 running.")).toHaveClass("sr-only")
    const table = screen.getByRole("table", { name: "Activity data" })
    expect(within(table).getAllByRole("row")).toHaveLength(5)
  })

  it("keeps the last data when a refresh fails, and offers to retry", async () => {
    const props = draw({ error: new Error("boom") })
    expect(screen.getByRole("alert")).toHaveTextContent("Couldn't load activity: boom")
    expect(screen.getByRole("group", { name: "Runner lanes for the last hour" })).toBeInTheDocument()
    await userEvent.setup().click(screen.getByRole("button", { name: "Retry" }))
    expect(props.onRetry).toHaveBeenCalled()
  })

  it("waits for the first answer", () => {
    draw({ activity: undefined })
    expect(screen.getByText("Loading activity")).toBeInTheDocument()
  })

  it("shows only the error when nothing has loaded", () => {
    draw({ activity: undefined, error: new Error("boom") })
    expect(screen.getByRole("alert")).toBeInTheDocument()
    expect(screen.queryByText("Loading activity")).toBeNull()
  })
})
