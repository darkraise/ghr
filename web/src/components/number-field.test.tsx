import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { useState } from "react"
import { describe, expect, it, vi } from "vitest"
import { numberFromText, textFromNumber } from "@/lib/number-text"
import { NumberField } from "./number-field"

function Harness({ initial = Number.NaN, precision, onValue }: { initial?: number; precision?: number; onValue?: (n: number) => void }) {
  const [value, setValue] = useState(initial)
  return (
    <NumberField
      label="Days"
      value={value}
      min={1}
      max={100}
      precision={precision}
      suffix="days"
      placeholder="default"
      onValueChange={(n) => {
        setValue(n)
        onValue?.(n)
      }}
    />
  )
}

describe("NumberField", () => {
  it("shows an empty value as an empty box with its placeholder and suffix", () => {
    render(<Harness />)
    const field = screen.getByRole("spinbutton", { name: "Days" })
    expect(field).toHaveValue("")
    expect(field).toHaveAttribute("placeholder", "default")
    expect(screen.getByText("days")).toBeInTheDocument()
  })

  it("announces the value with its unit, and no value text when empty", () => {
    const { rerender } = render(<NumberField label="Disk" value={80} suffix="%" onValueChange={() => {}} />)
    const field = screen.getByRole("spinbutton", { name: "Disk" })
    expect(field).toHaveAttribute("aria-valuetext", "80 %")
    rerender(<NumberField label="Disk" value={Number.NaN} suffix="%" onValueChange={() => {}} />)
    expect(field).not.toHaveAttribute("aria-valuetext")
    rerender(<NumberField label="Disk" value={10} suffix="s" spokenSuffix="seconds" onValueChange={() => {}} />)
    expect(field).toHaveAttribute("aria-valuetext", "10 seconds")
    rerender(<NumberField label="Disk" value={10} onValueChange={() => {}} />)
    expect(field).not.toHaveAttribute("aria-valuetext")
  })

  it("reports typed numbers, and NaN once emptied", async () => {
    const onValue = vi.fn()
    const user = userEvent.setup()
    render(<Harness initial={5} onValue={onValue} />)
    const field = screen.getByRole("spinbutton", { name: "Days" })
    await user.clear(field)
    expect(onValue).toHaveBeenLastCalledWith(Number.NaN)
    await user.type(field, "12")
    expect(onValue).toHaveBeenLastCalledWith(12)
  })

  it("pulls a value past the limits back when the field loses focus", async () => {
    const onValue = vi.fn()
    const user = userEvent.setup()
    render(<Harness onValue={onValue} />)
    const field = screen.getByRole("spinbutton", { name: "Days" })
    await user.type(field, "150")
    expect(onValue).toHaveBeenLastCalledWith(150)
    await user.tab()
    expect(onValue).toHaveBeenLastCalledWith(100)
    expect(field).toHaveValue("100")
  })

  it("reports whole numbers while a fraction is typed into a whole-number field", async () => {
    const onValue = vi.fn()
    const user = userEvent.setup()
    render(<Harness precision={0} onValue={onValue} />)
    const field = screen.getByRole("spinbutton", { name: "Days" })
    await user.type(field, "30.5")
    expect(onValue).toHaveBeenLastCalledWith(30)
    expect(field).toHaveValue("30.5")
    await user.tab()
    expect(onValue).toHaveBeenLastCalledWith(30)
    expect(field).toHaveValue("30")
  })

  it("steps with buttons named after the field", async () => {
    const user = userEvent.setup()
    render(<Harness initial={5} />)
    await user.click(screen.getByRole("button", { name: "Increase Days" }))
    expect(screen.getByRole("spinbutton", { name: "Days" })).toHaveValue("6")
    await user.click(screen.getByRole("button", { name: "Decrease Days" }))
    expect(screen.getByRole("spinbutton", { name: "Days" })).toHaveValue("5")
  })

  it("converts between a text field's value and a number", () => {
    expect(numberFromText("")).toBeNaN()
    expect(numberFromText(" 7 ")).toBe(7)
    expect(textFromNumber(Number.NaN)).toBe("")
    expect(textFromNumber(7)).toBe("7")
  })
})
