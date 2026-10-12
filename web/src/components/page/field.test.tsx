import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { Input } from "darkraise-ui/components/input"
import { describe, expect, it, vi } from "vitest"
import { NumberField } from "@/components/number-field"
import { TagField } from "@/components/tag-field"
import { ErrorLine } from "./error-line"
import { Field, FieldGroup, FieldInput } from "./field"

describe("FieldGroup", () => {
  it("is a region named by its heading, which takes no focus", () => {
    render(
      <FieldGroup id="timing" title="Timing" note="Applies at once">
        <p>rows</p>
      </FieldGroup>,
    )
    const region = screen.getByRole("region", { name: "Timing" })
    expect(region).toHaveAttribute("id", "timing")
    const heading = screen.getByRole("heading", { name: "Timing", level: 2 })
    expect(heading).toHaveAttribute("id", "timing-title")
    expect(heading).not.toHaveAttribute("tabindex")
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

  it("describes its control by the help and the error, and marks it invalid", () => {
    render(
      <Field label="Poll interval" htmlFor="poll" help="How often GitHub is checked" error="Enter a duration">
        <FieldInput id="poll" />
      </Field>,
    )
    const input = screen.getByLabelText("Poll interval")
    expect(input).toHaveAttribute("aria-invalid", "true")
    expect(input).toHaveAccessibleDescription("How often GitHub is checked Enter a duration")
  })

  it("keeps the field links when the caller adds its own description", () => {
    render(
      <Field label="Poll interval" htmlFor="poll" help="How often GitHub is checked">
        <FieldInput id="poll" aria-describedby="extra" />
        <p id="extra">At least 5s</p>
      </Field>,
    )
    expect(screen.getByLabelText("Poll interval")).toHaveAccessibleDescription("How often GitHub is checked At least 5s")
  })

  it("describes its control by the help alone while there is no error", () => {
    render(
      <Field label="Poll interval" htmlFor="poll" help="How often GitHub is checked">
        <FieldInput id="poll" />
      </Field>,
    )
    const input = screen.getByLabelText("Poll interval")
    expect(input).not.toHaveAttribute("aria-invalid")
    expect(input).toHaveAccessibleDescription("How often GitHub is checked")
  })

  it("links number and tag fields too, keeping a description passed to a number field", () => {
    render(
      <>
        <p id="extra">Also this</p>
        <Field label="Levels" help="Shares of the allowance" error="Critical must be above warning">
          <NumberField label="Warning at" value={80} onValueChange={() => {}} aria-describedby="extra" />
        </Field>
        <Field label="Labels" help="Added to every runner" error="Too long">
          <TagField label="Labels" value={[]} onChange={() => {}} />
        </Field>
      </>,
    )
    const number = screen.getByRole("spinbutton", { name: "Warning at" })
    expect(number).toHaveAttribute("aria-invalid", "true")
    expect(number).toHaveAccessibleDescription("Shares of the allowance Critical must be above warning Also this")
    const tag = screen.getByRole("textbox", { name: "Labels" })
    expect(tag).toHaveAttribute("aria-invalid", "true")
    expect(tag).toHaveAccessibleDescription("Added to every runner Too long")
  })

  it("adds nothing to a control outside a field", () => {
    render(<FieldInput aria-label="Free" />)
    const input = screen.getByRole("textbox", { name: "Free" })
    expect(input).not.toHaveAttribute("aria-describedby")
    expect(input).not.toHaveAttribute("aria-invalid")
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
