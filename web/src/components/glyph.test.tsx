import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { Glyph } from "./glyph"

describe("Glyph", () => {
  it("names the symbol for assistive technology", () => {
    render(<Glyph symbol="✔" label="succeeded" className="text-green-600" />)
    const glyph = screen.getByRole("img", { name: "succeeded" })
    expect(glyph).toHaveTextContent("✔")
    expect(glyph).toHaveClass("text-green-600")
  })
})
