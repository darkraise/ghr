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

  it("draws a header band above a padded body", () => {
    render(
      <Section title="Events" aside={<span>aside</span>}>
        <p>body</p>
      </Section>,
    )
    const heading = screen.getByRole("heading", { level: 2, name: "Events" })
    expect(heading.parentElement).toHaveClass("bg-background", "border-b")
    expect(heading.parentElement).toContainElement(screen.getByText("aside"))
    expect(screen.getByText("body").parentElement).toHaveClass("p-4")
  })

  it("drops the body padding when flush", () => {
    render(
      <Section title="Runs" flush>
        <p>body</p>
      </Section>,
    )
    expect(screen.getByText("body").parentElement).not.toHaveClass("p-4")
  })
})
