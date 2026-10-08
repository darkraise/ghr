import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { Section } from "./section"

describe("Section", () => {
  it("is a region named by its heading, not by aria-label", () => {
    render(
      <Section title="Disk" aside={<span>aside</span>}>
        <p>body</p>
      </Section>,
    )
    const region = screen.getByRole("region", { name: "Disk" })
    const heading = screen.getByRole("heading", { name: "Disk", level: 2 })
    expect(region).toHaveAttribute("aria-labelledby", heading.id)
    expect(region).not.toHaveAttribute("aria-label")
    expect(screen.getByText("aside")).toBeInTheDocument()
    expect(screen.getByText("body")).toBeInTheDocument()
  })
})
