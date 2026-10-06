import { render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { copyText } from "@/lib/clipboard"
import { Sparkline } from "./sparkline"
import { stateVariant } from "@/lib/status"
import { StateBadge } from "./state-badge"

function secure(value: boolean) {
  Object.defineProperty(window, "isSecureContext", { value, configurable: true })
}

afterEach(() => {
  Reflect.deleteProperty(navigator, "clipboard")
  Reflect.deleteProperty(document, "execCommand")
})

describe("StateBadge", () => {
  it("colours states as the TUI does", () => {
    expect(stateVariant("busy")).toBe("blue")
    expect(stateVariant("success")).toBe("green")
    expect(stateVariant("idle")).toBe("amber")
    expect(stateVariant("failure")).toBe("red")
    expect(stateVariant("cleaning")).toBe("secondary")
  })
  it("shows the state", () => {
    render(<StateBadge state="busy" />)
    expect(screen.getByText("busy")).toBeInTheDocument()
  })
})

describe("Sparkline", () => {
  it("breaks the line at gaps", () => {
    const { container } = render(<Sparkline label="running" values={[1, 2, null, 3, 4]} max={4} />)
    expect(screen.getByRole("img", { name: "running" })).toBeInTheDocument()
    expect(container.querySelectorAll("polyline")).toHaveLength(2)
  })
  it("draws nothing without values", () => {
    const { container } = render(<Sparkline label="cpu" values={[null, null]} />)
    expect(container.querySelectorAll("polyline")).toHaveLength(0)
  })
})

describe("copyText", () => {
  it("uses the Clipboard API in a secure context", async () => {
    secure(true)
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    await copyText("aaaaaa")
    expect(writeText).toHaveBeenCalledWith("aaaaaa")
  })

  it("falls back to execCommand over plain HTTP", async () => {
    secure(false)
    const exec = vi.fn(() => true)
    Object.defineProperty(document, "execCommand", { value: exec, configurable: true })
    await copyText("bbbbbb")
    expect(exec).toHaveBeenCalledWith("copy")
    expect(document.querySelector("textarea")).toBeNull()
  })

  it("reports a refused copy", async () => {
    secure(false)
    Object.defineProperty(document, "execCommand", { value: () => false, configurable: true })
    await expect(copyText("cccccc")).rejects.toThrow("the browser refused to copy")
  })
})
