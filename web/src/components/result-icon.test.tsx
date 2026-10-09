import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { ResultIcon } from "./result-icon"

describe("ResultIcon", () => {
  it.each([
    ["success", "Succeeded", "text-success"],
    ["failure", "Failed", "text-destructive"],
    ["cancelled", "Cancelled", "text-muted-foreground"],
    ["skipped", "Skipped", "text-muted-foreground"],
    ["unknown", "Unknown", "text-muted-foreground"],
    ["timed_out", "Failed", "text-destructive"],
  ])("draws %s", (conclusion, label, colour) => {
    render(<ResultIcon conclusion={conclusion} />)
    expect(screen.getByRole("img", { name: label })).toHaveClass(colour)
  })
})
