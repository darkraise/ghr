import { fireEvent, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { useState } from "react"
import { describe, expect, it } from "vitest"
import { TagField } from "./tag-field"

function Tags({ maxLength }: { maxLength?: number }) {
  const [value, setValue] = useState<string[]>([])
  return <TagField label="Hosts" value={value} onChange={setValue} maxLength={maxLength} />
}

const box = () => screen.getByRole("textbox", { name: "Hosts" })

describe("TagField length", () => {
  it("adds a long hostname when the field allows it", async () => {
    const host = `${"a".repeat(116)}.lan`
    expect(host).toHaveLength(120)
    render(<Tags maxLength={253} />)
    fireEvent.change(box(), { target: { value: host } })
    await userEvent.setup().click(screen.getByRole("button", { name: "Add to Hosts" }))
    expect(screen.getByRole("button", { name: `Remove ${host}` })).toBeInTheDocument()
    expect(box()).toHaveValue("")
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("keeps a too-long tag in the box and says why", async () => {
    const label = "b".repeat(101)
    render(<Tags />)
    fireEvent.change(box(), { target: { value: label } })
    await userEvent.setup().click(screen.getByRole("button", { name: "Add to Hosts" }))
    expect(box()).toHaveValue(label)
    expect(screen.getByRole("alert")).toHaveTextContent("At most 100 characters")
    expect(box()).toHaveAccessibleDescription("At most 100 characters")
    expect(screen.queryByRole("button", { name: /^Remove / })).toBeNull()
  })

  it("counts characters, not UTF-16 units", async () => {
    render(<Tags maxLength={3} />)
    fireEvent.change(box(), { target: { value: "😀😀😀" } })
    await userEvent.setup().click(screen.getByRole("button", { name: "Add to Hosts" }))
    expect(screen.getByRole("button", { name: "Remove 😀😀😀" })).toBeInTheDocument()
  })
})
