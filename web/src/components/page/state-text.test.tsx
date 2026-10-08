import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { stateTone } from "@/lib/status"
import { StateText } from "./state-text"

describe("stateTone", () => {
  it.each([
    ["busy", "accent"],
    ["Running", "accent"],
    ["online", "accent"],
    ["matched", "accent"],
    ["waiting", "warn"],
    ["starting", "warn"],
    ["expires soon", "warn"],
    ["refused", "warn"],
    ["failure", "bad"],
    ["offline", "bad"],
    ["rejected", "bad"],
    ["ok", "ok"],
    ["up to date", "ok"],
    ["paused", "muted"],
    ["idle", "muted"],
    ["cancelled", "muted"],
    ["finished", "muted"],
  ])("reads %s as %s", (state, tone) => {
    expect(stateTone(state)).toBe(tone)
  })
})

describe("StateText", () => {
  it("shows the state as a sentence-case word in its tone's colour", () => {
    render(
      <>
        <StateText state="busy" />
        <StateText state="offline" label="Offline now" />
        <StateText state="paused" />
      </>,
    )
    expect(screen.getByText("Busy")).toHaveClass("text-primary")
    expect(screen.getByText("Offline now")).toHaveClass("text-destructive")
    expect(screen.getByText("Paused")).toHaveClass("text-muted-foreground")
  })
})
