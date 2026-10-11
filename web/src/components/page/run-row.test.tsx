import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { runCols } from "@/lib/run-cols"
import { RunGroup, RunRow, RunValue } from "./run-row"

function table(wide: boolean, actions = false) {
  return render(
    <table>
      <tbody>
        <RunGroup label="Today" cols={runCols(wide, actions)} />
        <RunRow
          wide={wide}
          icon={<span>icon</span>}
          title={<a href="/x">build</a>}
          facts={<span>CI #7</span>}
          values={
            <>
              <RunValue kind="time">14:02</RunValue>
              <RunValue kind="duration">3m10s</RunValue>
            </>
          }
          actions={actions ? <button type="button">Open</button> : undefined}
        />
      </tbody>
    </table>,
  )
}

const cells = () => within(screen.getAllByRole("row")[1] as HTMLElement).getAllByRole("cell")

describe("RunRow", () => {
  it("puts the values in their own right-aligned cell when wide", () => {
    table(true)
    expect(cells()).toHaveLength(3)
    expect(cells()[1]).toHaveTextContent("buildCI #7")
    expect(cells()[2]).toHaveTextContent("14:023m10s")
    expect(cells()[2]).toHaveClass("text-right")
    expect(cells()[2]?.firstElementChild).toHaveClass("items-end")
  })

  it("moves the values under the facts when narrow", () => {
    table(false)
    expect(cells()).toHaveLength(2)
    expect(cells()[1]).toHaveTextContent("buildCI #714:023m10s")
  })

  it("adds an actions cell only when actions are given", () => {
    table(true, true)
    expect(cells()).toHaveLength(4)
    expect(screen.getByRole("button", { name: "Open" })).toBeInTheDocument()
  })

  it("leaves out the values cell when no values are given", () => {
    render(
      <table>
        <tbody>
          <RunRow wide icon={<span>icon</span>} title="build" />
        </tbody>
      </table>,
    )
    expect(screen.getAllByRole("cell")).toHaveLength(2)
  })

  it.each([true, false])("keeps a zero inside its own wrapper (wide: %s)", (wide) => {
    render(
      <table>
        <tbody>
          <RunRow wide={wide} icon={<span>icon</span>} title="build" facts={0} values={0} actions={0} />
        </tbody>
      </table>,
    )
    const row = screen.getByRole("row")
    const found = within(row).getAllByRole("cell")
    expect([...row.childNodes].every((n) => n.nodeName === "TD")).toBe(true)
    expect(found).toHaveLength(wide ? 4 : 3)
    expect([...(found[1] as HTMLElement).childNodes].every((n) => n.nodeName === "SPAN")).toBe(true)
    expect(found[1]).toHaveTextContent(wide ? "build0" : "build00")
    expect(found.at(-1)).toHaveTextContent("0")
  })

  it("spans the group heading across the row", () => {
    table(true, true)
    const heading = screen.getByRole("rowheader", { name: "Today" })
    expect(heading).toHaveAttribute("scope", "rowgroup")
    expect(heading).toHaveAttribute("colspan", "4")
  })

  it("counts the columns", () => {
    expect([runCols(true, false), runCols(true, true), runCols(false, false), runCols(false, true)]).toEqual([3, 4, 2, 3])
  })
})
