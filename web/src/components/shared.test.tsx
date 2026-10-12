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

function Tags({ initial = [] as string[], exact = false }) {
  const [value, setValue] = useState(initial)
  return <TagField label="Labels" value={value} onChange={setValue} exact={exact} />
}

const tags = () => screen.getAllByRole("button", { name: /^Remove / }).map((b) => b.getAttribute("aria-label"))

async function enter(user: ReturnType<typeof userEvent.setup>, text: string) {
  await user.click(screen.getByRole("textbox", { name: "Labels" }))
  await user.paste(text)
  await user.keyboard("{Enter}")
}

describe("TagField", () => {
  it("adds trimmed tags by Enter and by the button", async () => {
    const user = userEvent.setup()
    render(<Tags initial={["gpu"]} />)
    await user.type(screen.getByRole("textbox", { name: "Labels" }), "  big {Enter}")
    await user.type(screen.getByRole("textbox", { name: "Labels" }), "fast")
    await user.click(screen.getByRole("button", { name: "Add to Labels" }))
    expect(tags()).toEqual(["Remove gpu", "Remove big", "Remove fast"])
    expect(screen.getByRole("textbox", { name: "Labels" })).toHaveValue("")
  })

  it.each([
    ["a duplicate", "gpu", "Already listed as gpu"],
    ["a duplicate in another case", "GPU", "Already listed as gpu"],
  ])("refuses %s, keeps the text and says why", async (_what, typed, reason) => {
    const user = userEvent.setup()
    render(<Tags initial={["gpu"]} />)
    await enter(user, typed)
    const box = screen.getByRole("textbox", { name: "Labels" })
    expect(box).toHaveValue(typed)
    expect(screen.getByText(reason)).toBeInTheDocument()
    expect(tags()).toEqual(["Remove gpu"])
    await user.type(box, "x")
    expect(screen.queryByText(reason)).toBeNull()
  })

  it("drops the reason once the tag is added", async () => {
    const user = userEvent.setup()
    render(<Tags initial={["gpu"]} />)
    await enter(user, "GPU")
    expect(screen.getByText("Already listed as gpu")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Remove gpu" }))
    await user.click(screen.getByRole("button", { name: "Add to Labels" }))
    expect(tags()).toEqual(["Remove GPU"])
    expect(screen.queryByText("Already listed as gpu")).toBeNull()
  })

  it("compares exactly when asked to", async () => {
    const user = userEvent.setup()
    render(<Tags initial={["test_"]} exact />)
    await enter(user, "Test_")
    expect(tags()).toEqual(["Remove test_", "Remove Test_"])
    await enter(user, "test_")
    expect(screen.getByText("Already listed as test_")).toBeInTheDocument()
    expect(tags()).toEqual(["Remove test_", "Remove Test_"])
  })

  it("removes a tag", async () => {
    const user = userEvent.setup()
    render(<Tags initial={["gpu", "big"]} />)
    await user.click(screen.getByRole("button", { name: "Remove gpu" }))
    expect(screen.queryByText("gpu")).toBeNull()
    expect(screen.getByText("big")).toBeInTheDocument()
  })
})
