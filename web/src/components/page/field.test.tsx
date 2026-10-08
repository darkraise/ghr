import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { Input } from "darkraise-ui/components/input"
import { describe, expect, it, vi } from "vitest"
import { ErrorLine } from "./error-line"
import { Field, FieldGroup } from "./field"

describe("FieldGroup", () => {
  it("is a region named by a heading the section index can focus", () => {
    render(
      <FieldGroup id="timing" title="Timing" note="Applies at once">
        <p>rows</p>
      </FieldGroup>,
    )
    const region = screen.getByRole("region", { name: "Timing" })
    expect(region).toHaveAttribute("id", "timing")
    const heading = screen.getByRole("heading", { name: "Timing", level: 2 })
    expect(heading).toHaveAttribute("id", "timing-title")
    expect(heading).toHaveAttribute("tabindex", "-1")
    expect(screen.getByText("Applies at once")).toBeInTheDocument()
  })
})

describe("Field", () => {
  it("labels its control and shows help, an error and a changed mark", () => {
    render(
      <Field label="Poll interval" htmlFor="poll" help="How often GitHub is checked" error="Enter a duration" changed>
        <Input id="poll" />
      </Field>,
    )
    expect(screen.getByLabelText("Poll interval")).toHaveAttribute("id", "poll")
    expect(screen.getByText("How often GitHub is checked")).toHaveClass("text-muted-foreground")
    expect(screen.getByText("Enter a duration")).toHaveClass("text-destructive")
    expect(screen.getByText("Changed")).toHaveClass("text-warning")
  })

  it("renders a read-only row's label as text, not a label element", () => {
    const { container } = render(
      <Field label="Owner">
        <span>darkraise</span>
      </Field>,
    )
    expect(container.querySelector("label")).toBeNull()
    expect(screen.getByText("Owner")).toBeInTheDocument()
    expect(screen.queryByText("Changed")).toBeNull()
  })
})

describe("ErrorLine", () => {
  it("announces the error and offers a retry when given one", async () => {
    const onRetry = vi.fn()
    render(<ErrorLine onRetry={onRetry}>history file unreadable</ErrorLine>)
    expect(screen.getByRole("alert")).toHaveTextContent("history file unreadable")
    await userEvent.setup().click(screen.getByRole("button", { name: "Try again" }))
    expect(onRetry).toHaveBeenCalledOnce()
  })

  it("has no retry button without onRetry", () => {
    render(<ErrorLine>boom</ErrorLine>)
    expect(screen.queryByRole("button")).toBeNull()
  })
})
