import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { RejectedAlert, SaveBar } from "./save-bar"

describe("SaveBar", () => {
  it("counts the changes, then saves or discards", async () => {
    const onSave = vi.fn()
    const onDiscard = vi.fn()
    render(<SaveBar count={2} saving={false} onSave={onSave} onDiscard={onDiscard} />)
    expect(screen.getByText("2 unsaved changes")).toHaveClass("text-warning")
    const user = userEvent.setup()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await user.click(screen.getByRole("button", { name: "Discard" }))
    expect(onSave).toHaveBeenCalledOnce()
    expect(onDiscard).toHaveBeenCalledOnce()
  })

  it("renders nothing without changes", () => {
    const { container } = render(<SaveBar count={0} saving={false} onSave={() => {}} onDiscard={() => {}} />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe("RejectedAlert", () => {
  it("lists three reasons and counts the rest", () => {
    render(<RejectedAlert message="a is bad; b is bad; c is bad; d is bad; e is bad" />)
    expect(screen.getByText("a is bad")).toBeInTheDocument()
    expect(screen.queryByText("d is bad")).toBeNull()
    expect(screen.getByText("and 2 more")).toBeInTheDocument()
    expect(screen.queryByText(/✖|…/)).toBeNull()
  })
})
