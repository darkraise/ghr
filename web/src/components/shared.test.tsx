import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { useState } from "react"
import { describe, expect, it, vi } from "vitest"
import { ConfirmDialog, type Confirm } from "./confirm-dialog"
import { TagField } from "./tag-field"

function Asker({ run }: { run: () => void }) {
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  return (
    <>
      <button onClick={() => setConfirm({ title: "Remove repo darkmem? Its running jobs finish first.", action: "Remove", destructive: true, run })}>
        ask
      </button>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}

describe("ConfirmDialog", () => {
  it("runs nothing on Cancel", async () => {
    const run = vi.fn()
    const user = userEvent.setup()
    render(<Asker run={run} />)
    await user.click(screen.getByRole("button", { name: "ask" }))
    expect(await screen.findByText("Remove repo darkmem? Its running jobs finish first.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(run).not.toHaveBeenCalled()
  })

  it("runs the action once, closes, and never shows a blank title", async () => {
    const run = vi.fn()
    const user = userEvent.setup()
    render(<Asker run={run} />)
    await user.click(screen.getByRole("button", { name: "ask" }))
    await user.click(await screen.findByRole("button", { name: "Remove" }))
    expect(run).toHaveBeenCalledTimes(1)
    expect(screen.queryByText(/undefined/)).toBeNull()
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
  })
})

function Tags({ initial = [] as string[] }) {
  const [value, setValue] = useState(initial)
  return <TagField label="Labels" value={value} onChange={setValue} />
}

describe("TagField", () => {
  it("adds trimmed tags by Enter and by the button, skipping duplicates", async () => {
    const user = userEvent.setup()
    render(<Tags initial={["gpu"]} />)
    await user.type(screen.getByRole("textbox", { name: "Labels" }), "  big {Enter}")
    await user.type(screen.getByRole("textbox", { name: "Labels" }), "gpu")
    await user.click(screen.getByRole("button", { name: "Add to Labels" }))
    expect(screen.getAllByRole("button", { name: /^Remove / }).map((b) => b.getAttribute("aria-label"))).toEqual([
      "Remove gpu",
      "Remove big",
    ])
    expect(screen.getByRole("textbox", { name: "Labels" })).toHaveValue("")
  })

  it("removes a tag", async () => {
    const user = userEvent.setup()
    render(<Tags initial={["gpu", "big"]} />)
    await user.click(screen.getByRole("button", { name: "Remove gpu" }))
    expect(screen.queryByText("gpu")).toBeNull()
    expect(screen.getByText("big")).toBeInTheDocument()
  })
})
