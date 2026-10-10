import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { stateTone } from "@/lib/status"
import { StateText } from "./state-text"

describe("stateTone", () => {
  it.each([
    ["busy", "run"],
    ["Running", "run"],
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
  it("shows the state as a sentence-case word, or a given label", () => {
    render(
      <>
        <StateText state="busy" />
        <StateText state="offline" label="Offline now" />
        <StateText state="paused" />
      </>,
    )
    expect(screen.getByText("Running")).toBeInTheDocument()
    expect(screen.getByText("Offline now")).toBeInTheDocument()
    expect(screen.getByText("Paused")).toBeInTheDocument()
  })
})
