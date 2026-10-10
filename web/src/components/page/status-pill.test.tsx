import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { ResultIcon } from "@/components/result-icon"
import { RunStatusIcon } from "@/components/run-status-icon"
import { stateTone, stateWord } from "@/lib/status"
import { StatusPill } from "./status-pill"

describe("the status vocabulary", () => {
  it.each([
    ["success", "Succeeded", "ok", "text-success", "lucide-check"],
    ["succeeded", "Succeeded", "ok", "text-success", "lucide-check"],
    ["failure", "Failed", "bad", "text-destructive", "lucide-x"],
    ["failed", "Failed", "bad", "text-destructive", "lucide-x"],
    ["error", "Error", "bad", "text-destructive", "lucide-x"],
    ["running", "Running", "run", "text-running", "lucide-circle-dot"],
    ["busy", "Running", "run", "text-running", "lucide-circle-dot"],
    ["in_progress", "Running", "run", "text-running", "lucide-circle-dot"],
    ["queued", "Waiting", "warn", "text-warning", "lucide-clock"],
    ["waiting", "Waiting", "warn", "text-warning", "lucide-clock"],
    ["cancelled", "Cancelled", "muted", "text-muted-foreground", "lucide-ban"],
    ["skipped", "Skipped", "muted", "text-muted-foreground", "lucide-minus"],
    ["idle", "Idle", "muted", "text-muted-foreground", "lucide-minus"],
    ["paused", "Paused", "muted", "text-muted-foreground", "lucide-minus"],
    ["online", "Online", "accent", "text-primary", "lucide-dot"],
  ])("draws %s as %s", (state, word, tone, iconClass, shape) => {
    expect(stateWord(state)).toBe(word)
    expect(stateTone(state)).toBe(tone)
    const { container } = render(<StatusPill state={state} />)
    expect(screen.getByText(word)).toHaveClass("text-foreground")
    const icon = container.querySelector("svg")
    expect(icon).toHaveClass(iconClass, shape)
    expect(icon).toHaveAttribute("aria-hidden", "true")
  })

  it.each([
    ["Running", "run"],
    ["matched", "accent"],
    ["starting", "warn"],
    ["expires soon", "warn"],
    ["refused", "warn"],
    ["offline", "bad"],
    ["rejected", "bad"],
    ["unmatched", "bad"],
    ["valid", "ok"],
    ["pending", "warn"],
    ["requested", "warn"],
    ["in_progress", "run"],
    ["stopping", "muted"],
    ["ok", "ok"],
    ["up to date", "ok"],
    ["finished", "muted"],
  ])("reads %s as %s", (state, tone) => {
    expect(stateTone(state)).toBe(tone)
  })

  it("shows a given label in place of the state's word", () => {
    render(<StatusPill state="success" label="Up to date" />)
    expect(screen.getByText("Up to date")).toBeInTheDocument()
    expect(screen.queryByText("Succeeded")).not.toBeInTheDocument()
  })

  it.each([
    ["success", "Succeeded", "text-success"],
    ["failure", "Failed", "text-destructive"],
    ["cancelled", "Cancelled", "text-muted-foreground"],
    ["skipped", "Skipped", "text-muted-foreground"],
    ["unknown", "Unknown", "text-muted-foreground"],
    ["timed_out", "Failed", "text-destructive"],
  ])("names a %s result %s", (conclusion, word, iconClass) => {
    render(<ResultIcon conclusion={conclusion} />)
    expect(screen.getByRole("img", { name: word })).toHaveClass(iconClass)
  })

  it("names a queued run Waiting, in the waiting colour", () => {
    render(<RunStatusIcon status="queued" conclusion="" />)
    expect(screen.getByRole("img", { name: "Waiting" })).toHaveClass("text-warning")
  })

  it("keeps the spinner for a run in progress", () => {
    render(<RunStatusIcon status="in_progress" conclusion="" />)
    expect(screen.getByText("Running")).toHaveClass("sr-only")
  })
})
