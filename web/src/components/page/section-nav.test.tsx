import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router"
import { fireEvent, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { setViewport } from "@/test/media"
import { FieldGroup } from "./field"
import { SectionNav } from "./section-nav"

const ITEMS = [
  { id: "general", title: "General" },
  { id: "timing", title: "Timing" },
] as const

function Page() {
  return (
    <>
      <SectionNav items={ITEMS} />
      <FieldGroup id="general" title="General">
        <p>general rows</p>
      </FieldGroup>
      <FieldGroup id="timing" title="Timing">
        <p>timing rows</p>
      </FieldGroup>
    </>
  )
}

function placeAt(id: string, top: number) {
  const el = document.getElementById(id)
  if (!el) throw new Error(`no #${id}`)
  vi.spyOn(el, "getBoundingClientRect").mockReturnValue({ top } as DOMRect)
}

function layout(tops: Record<string, number>) {
  vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(function (this: Element) {
    return { top: tops[this.id] ?? 500 } as DOMRect
  })
}

describe("SectionNav", () => {
  afterEach(() => vi.restoreAllMocks())

  it("is not shown below 1280px", () => {
    setViewport(1024)
    render(<Page />)
    expect(screen.queryByRole("navigation", { name: "Sections" })).toBeNull()
  })

  it("scrolls to a section and focuses its heading", async () => {
    setViewport(1280)
    const scroll = vi.spyOn(Element.prototype, "scrollIntoView")
    render(<Page />)
    await userEvent.setup().click(screen.getByRole("button", { name: "Timing" }))
    expect(scroll).toHaveBeenCalled()
    expect(scroll.mock.contexts.at(-1)).toBe(document.getElementById("timing"))
    expect(document.activeElement).toBe(screen.getByRole("heading", { name: "Timing" }))
  })

  it("works out the current section on mount, before any scroll", () => {
    setViewport(1280)
    layout({ general: -400, timing: 40 })
    render(<Page />)
    expect(screen.getByRole("button", { name: "Timing" })).toHaveAttribute("aria-current", "true")
  })

  it("marks the section nearest the top as current while scrolling", () => {
    setViewport(1280)
    layout({})
    render(<Page />)
    expect(screen.getByRole("button", { name: "General" })).toHaveAttribute("aria-current", "true")
    placeAt("general", -400)
    placeAt("timing", 40)
    fireEvent.scroll(document)
    expect(screen.getByRole("button", { name: "Timing" })).toHaveAttribute("aria-current", "true")
    expect(screen.getByRole("button", { name: "General" })).not.toHaveAttribute("aria-current")
  })
})

describe("SectionNav beside unsaved changes", () => {
  it("jumps without opening the unsaved-changes dialog or changing the URL", async () => {
    setViewport(1280)
    function Form() {
      return (
        <>
          <Page />
          <UnsavedGuard count={1} page="Settings" saving={false} onSave={async () => true} onDiscard={() => {}} />
        </>
      )
    }
    const root = createRootRoute({ component: () => <Outlet /> })
    const form = createRoute({ getParentRoute: () => root, path: "/settings", component: Form })
    const router = createRouter({ routeTree: root.addChildren([form]), history: createMemoryHistory({ initialEntries: ["/settings"] }) })
    render(<RouterProvider router={router} />)
    await userEvent.setup().click(await screen.findByRole("button", { name: "Timing" }))
    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(screen.queryByText("You have 1 unsaved change on the Settings page.")).toBeNull()
    expect(router.state.location.href).toBe("/settings")
  })
})
