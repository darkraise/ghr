import type { MouseEvent } from "react"
import { describe, expect, it } from "vitest"
import { plainClick } from "./plain-click"

const click = (over: Partial<MouseEvent>) => ({ button: 0, metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, ...over }) as MouseEvent

describe("plainClick", () => {
  it("takes a primary click with no modifier", () => {
    expect(plainClick(click({}))).toBe(true)
  })

  it.each([
    ["a middle click", { button: 1 }],
    ["a secondary click", { button: 2 }],
    ["Cmd", { metaKey: true }],
    ["Ctrl", { ctrlKey: true }],
    ["Shift", { shiftKey: true }],
    ["Alt", { altKey: true }],
  ])("leaves %s to the browser", (_name, over) => {
    expect(plainClick(click(over))).toBe(false)
  })
})
