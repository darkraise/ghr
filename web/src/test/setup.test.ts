import { getConfig } from "@testing-library/react"
import { describe, expect, it } from "vitest"

describe("test setup", () => {
  it("gives async queries 3 seconds and stubs scrollIntoView", () => {
    expect(getConfig().asyncUtilTimeout).toBe(3000)
    expect(() => document.createElement("div").scrollIntoView()).not.toThrow()
  })
})
