import { describe, expect, it } from "vitest"
import { passwordLengthOk } from "./password"

describe("passwordLengthOk", () => {
  it("counts characters, not bytes or UTF-16 units", () => {
    expect(passwordLengthOk("é".repeat(11))).toBe(false)
    expect(passwordLengthOk("é".repeat(12))).toBe(true)
    expect(passwordLengthOk("😀".repeat(1024))).toBe(true)
    expect(passwordLengthOk("x".repeat(1025))).toBe(false)
  })
})
