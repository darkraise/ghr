import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { GhrEvent } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { EventList } from "./event-list"

const event = (seq: number, level = "info", msg = `event ${seq}`): GhrEvent => ({ seq, time: "2026-10-03T14:00:00Z", level, msg })

describe("EventList", () => {
  it("lists the newest first, with an icon for ok, warn and error only", () => {
    render(<EventList events={fixtures.events} />)
    const items = screen.getAllByRole("listitem")
    expect(items.map((li) => li.querySelector("time")?.textContent)).toEqual(["14:04", "14:03", "14:02"])
    expect(within(items[0] as HTMLElement).getByRole("img", { name: "Warning" })).toHaveClass("text-warning")
    expect(within(items[1] as HTMLElement).getByRole("img", { name: "OK" })).toHaveClass("text-success")
    expect(within(items[2] as HTMLElement).queryByRole("img")).toBeNull()
    expect(screen.getByText("runner aaaaaa started")).toBeInTheDocument()
    expect(screen.getByText("darkmem")).toHaveClass("text-muted-foreground")
  })

  it("marks errors", () => {
    render(<EventList events={[event(1, "error", "GitHub refused the token")]} />)
    expect(screen.getByRole("img", { name: "Error" })).toHaveClass("text-destructive")
  })

  it("shows the last 8", () => {
    render(<EventList events={Array.from({ length: 10 }, (_, i) => event(i + 1))} />)
    expect(screen.getAllByRole("listitem")).toHaveLength(8)
    expect(screen.getByText("event 10")).toBeInTheDocument()
    expect(screen.queryByText("event 2")).toBeNull()
  })

  it("says when there are none", () => {
    render(<EventList events={[]} />)
    expect(screen.getByText("No events yet.")).toBeInTheDocument()
  })
})
