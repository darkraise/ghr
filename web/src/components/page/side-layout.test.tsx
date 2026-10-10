import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { SIDE_LINK } from "@/lib/side-link"
import { SideGroup, SideLayout } from "./side-layout"

describe("SideLayout", () => {
  it.each([
    ["Settings", "General", "Host", "the general settings"],
    ["Repository", "Overview", "More", "the overview"],
  ])("names the %s navigation and shows the content beside it", (label, link, group, content) => {
    const { container } = render(
      <SideLayout
        label={label}
        nav={
          <>
            <a href="/a" aria-current="page" className={SIDE_LINK}>
              {link}
            </a>
            <SideGroup>{group}</SideGroup>
          </>
        }
      >
        <p>{content}</p>
      </SideLayout>,
    )
    const nav = screen.getByRole("navigation", { name: label })
    expect(within(nav).getByRole("link", { name: link })).toHaveAttribute("aria-current", "page")
    expect(within(nav).getByText(group)).toBeInTheDocument()
    expect(nav).not.toContainElement(screen.getByText(content))
    expect(container.firstElementChild).toHaveClass("grid", "min-[800px]:grid-cols-[15rem_minmax(0,1fr)]")
    expect(container.firstElementChild).not.toHaveClass("grid-cols-[15rem_minmax(0,1fr)]")
  })

  it("marks the current link by its aria-current attribute", () => {
    expect(SIDE_LINK).toContain("aria-[current=page]:bg-muted")
    expect(SIDE_LINK).toContain("aria-[current=page]:font-semibold")
    expect(SIDE_LINK).toContain("aria-[current=page]:shadow-[inset_2px_0_0_hsl(var(--primary))]")
  })
})
