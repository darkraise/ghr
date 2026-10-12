import { render } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { ansiSegments, matchRanges, parsedLine, plainText, renderAnsi } from "./ansi"

function html(raw: string, query?: string, current?: number): HTMLElement {
  return render(<p>{renderAnsi({ text: raw }, query, current)}</p>).container.firstElementChild as HTMLElement
}

describe("ansiSegments", () => {
  it("keeps plain text as one segment", () => {
    expect(ansiSegments("ruff check .")).toEqual([{ text: "ruff check .", color: undefined, bold: false }])
    expect(ansiSegments("")).toEqual([])
  })

  it("reads the 8 and bright 8 foreground colours, bold and resets", () => {
    expect(ansiSegments("\u001b[31mFound 1 error.\u001b[0m done")).toEqual([
      { text: "Found 1 error.", color: "red", bold: false },
      { text: " done", color: undefined, bold: false },
    ])
    expect(ansiSegments("\u001b[1;92mok\u001b[22m still green\u001b[39m plain")).toEqual([
      { text: "ok", color: "bright-green", bold: true },
      { text: " still green", color: "bright-green", bold: false },
      { text: " plain", color: undefined, bold: false },
    ])
    expect(ansiSegments("\u001b[1mbold\u001b[mreset")).toEqual([
      { text: "bold", color: undefined, bold: true },
      { text: "reset", color: undefined, bold: false },
    ])
  })

  it("skips 256-colour and true-colour arguments", () => {
    expect(ansiSegments("\u001b[38;5;31ma\u001b[38;2;31;32;33mb\u001b[48;5;1mc")).toEqual([
      { text: "abc", color: undefined, bold: false },
    ])
  })

  it.each([
    ["other CSI sequences", "\u001b[2K\u001b[1Gprogress\u001b[?25l", "progress"],
    ["an OSC hyperlink ended by ST", "\u001b]8;;https://example.com\u001b\\link\u001b]8;;\u001b\\", "link"],
    ["an OSC title ended by BEL", "\u001b]0;title\u0007text", "text"],
    ["an unterminated OSC", "text\u001b]0;title", "text"],
    ["other escapes", "a\u001b(Bb\u001b=c", "abc"],
    ["C0 controls other than tab", "a\u0000b\tc\u0007d\u0008e\u001b", "ab\tcde"],
  ])("removes %s", (_name, raw, want) => {
    expect(plainText(raw)).toBe(want)
  })

  it("shows only the last carriage-return segment of a line", () => {
    expect(plainText("10%\r50%\r100%")).toBe("100%")
    expect(plainText("done\r")).toBe("done")
    expect(plainText("\u001b[32m10%\r\u001b[31m100%")).toBe("100%")
  })
})

describe("parsedLine", () => {
  it("parses a line object once, however often it is asked for or rendered", () => {
    let reads = 0
    const line = {
      get text() {
        reads++
        return "\u001b[31mFound\u001b[0m 1 error."
      },
    }
    const parsed = parsedLine(line)
    expect(parsed).toEqual({
      segments: [
        { text: "Found", color: "red", bold: false },
        { text: " 1 error.", color: undefined, bold: false },
      ],
      text: "Found 1 error.",
    })
    expect(parsedLine(line)).toBe(parsed)
    render(<p>{renderAnsi(line, "found", 0)}</p>)
    render(<p>{renderAnsi(line, "error")}</p>)
    expect(reads).toBe(1)
  })
})

describe("matchRanges", () => {
  it("finds case-insensitive, non-overlapping occurrences", () => {
    expect(matchRanges("Error: error", "ERROR")).toEqual([
      [0, 5],
      [7, 12],
    ])
    expect(matchRanges("aaaa", "aa")).toEqual([
      [0, 2],
      [2, 4],
    ])
    expect(matchRanges("text", "")).toEqual([])
  })

  it("keeps offsets in the original text when lowercasing changes a length", () => {
    expect(matchRanges("İ ab", "ab")).toEqual([[2, 4]])
    expect(matchRanges("a.b a+b", "a+b")).toEqual([[4, 7]])
  })
})

describe("renderAnsi", () => {
  it("renders colours and bold as classed spans", () => {
    const p = html("\u001b[1;31mFAIL\u001b[0m test")
    const span = p.querySelector("span")
    expect(span).toHaveTextContent("FAIL")
    expect(span).toHaveClass("ghr-log-ansi-red", "font-semibold")
    expect(p).toHaveTextContent("FAIL test")
  })

  it("renders markup in the log as text", () => {
    const p = html("<img src=x onerror=alert(1)>")
    expect(p.querySelector("img")).toBeNull()
    expect(p).toHaveTextContent("<img src=x onerror=alert(1)>")
  })

  it("marks every occurrence of the query and flags the current one", () => {
    const p = html("Error: error", "error", 1)
    const marks = p.querySelectorAll("mark")
    expect([...marks].map((m) => m.textContent)).toEqual(["Error", "error"])
    expect(marks[0]).not.toHaveAttribute("data-current")
    expect(marks[1]).toHaveAttribute("data-current", "true")
    expect(marks[1]).toHaveClass("ghr-log-match")
  })

  it("marks an occurrence that crosses a colour change", () => {
    const p = html("\u001b[31mFou\u001b[0mnd it", "found", 0)
    const marks = p.querySelectorAll("mark")
    expect([...marks].map((m) => m.textContent)).toEqual(["Fou", "nd"])
    expect(marks[0]?.querySelector("span")).toHaveClass("ghr-log-ansi-red")
    expect(p).toHaveTextContent("Found it")
  })
})
