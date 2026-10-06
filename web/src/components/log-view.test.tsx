import { render } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { LogView } from "./log-view"

let sets: number[] = []

afterEach(() => {
  sets = []
  Reflect.deleteProperty(HTMLElement.prototype, "scrollHeight")
  Reflect.deleteProperty(HTMLElement.prototype, "scrollTop")
})

function trackScroll() {
  Object.defineProperty(HTMLElement.prototype, "scrollHeight", { configurable: true, get: () => 500 })
  Object.defineProperty(HTMLElement.prototype, "scrollTop", {
    configurable: true,
    get: () => 0,
    set: (v: number) => {
      sets.push(v)
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
})
