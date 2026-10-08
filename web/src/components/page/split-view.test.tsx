import { act, render, renderHook, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { useMediaQuery, WIDE, WIDEST } from "@/lib/use-media-query"
import { setViewport } from "@/test/media"
import { SplitView } from "./split-view"

const view = (panel: string | null = "panel") => (
  <SplitView list={<p>list</p>} panel={panel === null ? null : <p>{panel}</p>} narrow={<p>narrow</p>} />
)

describe("useMediaQuery", () => {
  it("matches min-width queries against the viewport", () => {
    setViewport(1100)
    expect(renderHook(() => useMediaQuery(WIDE)).result.current).toBe(true)
    expect(renderHook(() => useMediaQuery(WIDEST)).result.current).toBe(false)
  })
})

describe("SplitView", () => {
  it("puts the list beside the panel at 1024px and wider", () => {
    setViewport(1280)
    render(view())
    expect(screen.getByText("list")).toBeInTheDocument()
    expect(screen.getByText("panel")).toBeInTheDocument()
    expect(screen.queryByText("narrow")).toBeNull()
  })

  it("shows only the narrow content below 1024px", () => {
    setViewport(390)
    render(view())
    expect(screen.getByText("narrow")).toBeInTheDocument()
    expect(screen.queryByText("list")).toBeNull()
  })

  it("shows the list alone when there is no panel", () => {
    setViewport(1280)
    render(view(null))
    expect(screen.getByText("list")).toBeInTheDocument()
    expect(screen.queryByText("narrow")).toBeNull()
  })

  it("follows the width when it changes", () => {
    const media = setViewport(390)
    render(view())
    expect(screen.getByText("narrow")).toBeInTheDocument()
    act(() => media.resize(1280))
    expect(screen.getByText("panel")).toBeInTheDocument()
    expect(screen.queryByText("narrow")).toBeNull()
  })
})
