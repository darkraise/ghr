import { act, render, renderHook, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { useActivityWindow } from "@/lib/use-activity-window"
import { WindowControl } from "./window-control"

describe("WindowControl", () => {
  it("offers every window and reports the choice", async () => {
    const onChange = vi.fn()
    render(<WindowControl value="1h" onChange={onChange} />)
    expect(screen.getByRole("radiogroup", { name: "Time window" })).toBeInTheDocument()
    expect(screen.getAllByRole("radio").map((r) => r.textContent)).toEqual(["1h", "3h", "24h", "7d", "30d"])
    expect(screen.getByRole("radio", { name: "1h" })).toBeChecked()
    await userEvent.setup().click(screen.getByRole("radio", { name: "24h" }))
    expect(onChange).toHaveBeenCalledWith("24h")
  })

  it("offers only the windows it is given", async () => {
    const onChange = vi.fn()
    render(<WindowControl value="7d" onChange={onChange} options={["24h", "7d", "30d"] as const} />)
    expect(screen.getAllByRole("radio").map((r) => r.textContent)).toEqual(["24h", "7d", "30d"])
    await userEvent.setup().click(screen.getByRole("radio", { name: "30d" }))
    expect(onChange).toHaveBeenCalledWith("30d")
  })
})

describe("useActivityWindow", () => {
  beforeEach(() => localStorage.clear())

  it("starts at 1h and remembers the choice", () => {
    const { result } = renderHook(() => useActivityWindow())
    expect(result.current[0]).toBe("1h")
    act(() => result.current[1]("7d"))
    expect(result.current[0]).toBe("7d")
    expect(localStorage.getItem("ghr-activity-window")).toBe("7d")
    expect(renderHook(() => useActivityWindow()).result.current[0]).toBe("7d")
  })

  it("ignores a stored value that is not a window", () => {
    localStorage.setItem("ghr-activity-window", "2w")
    expect(renderHook(() => useActivityWindow()).result.current[0]).toBe("1h")
  })
})
