import { fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { LogView } from "./log-view"

let sets: number[] = []
let top = 0

afterEach(() => {
  sets = []
  top = 0
  Reflect.deleteProperty(HTMLElement.prototype, "scrollHeight")
  Reflect.deleteProperty(HTMLElement.prototype, "clientHeight")
  Reflect.deleteProperty(HTMLElement.prototype, "scrollTop")
})

function trackScroll() {
  Object.defineProperty(HTMLElement.prototype, "scrollHeight", { configurable: true, get: () => 500 })
  Object.defineProperty(HTMLElement.prototype, "clientHeight", { configurable: true, get: () => 100 })
  Object.defineProperty(HTMLElement.prototype, "scrollTop", {
    configurable: true,
    get: () => top,
    set: (v: number) => {
      sets.push(v)
      top = v
    },
  })
}

describe("LogView", () => {
  it("keeps the view at the end while following", () => {
    trackScroll()
    const { rerender } = render(<LogView text="one" follow />)
    rerender(<LogView text={"one\ntwo"} follow />)
    expect(sets).toEqual([500, 500])
  })

  it("leaves the scroll alone when not following", () => {
    trackScroll()
    const { rerender } = render(<LogView text="one" follow={false} />)
    rerender(<LogView text={"one\ntwo"} follow={false} />)
    expect(sets).toEqual([])
  })

  it("stops following when the user scrolls up, and follows again at the end", () => {
    trackScroll()
    const onFollowChange = vi.fn()
    const { container, rerender } = render(<LogView text="one" follow onFollowChange={onFollowChange} />)
    const pre = container.querySelector("pre")
    if (!pre) throw new Error("no log view")
    top = 100
    fireEvent.scroll(pre)
    expect(onFollowChange).toHaveBeenLastCalledWith(false)
    rerender(<LogView text="one" follow={false} onFollowChange={onFollowChange} />)
    top = 400
    fireEvent.scroll(pre)
    expect(onFollowChange).toHaveBeenLastCalledWith(true)
    expect(onFollowChange).toHaveBeenCalledTimes(2)
  })

  it("says when there is no output yet, outside the log itself", () => {
    const { container } = render(<LogView text="" follow />)
    const note = screen.getByText("No log output yet")
    expect(note.tagName).toBe("P")
    expect(container.querySelector("pre")).toBeEmptyDOMElement()
  })
})
