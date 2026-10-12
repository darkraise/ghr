import type { JobLog, LogLine } from "@/api/types"
import { matchRanges, parsedLine } from "@/lib/ansi"

// A step renders at most this many of its last lines until asked for more.
export const LOG_WINDOW = 5000

export type LogRow = { type: "line"; line: LogLine } | { type: "group"; line: LogLine; lines: LogLine[] }

// GitHub's groups do not nest: a group line closes any open group. endgroup
// lines are not shown, and a group without one runs to the end of the step.
export function groupRows(lines: LogLine[]): LogRow[] {
  const rows: LogRow[] = []
  let open: { type: "group"; line: LogLine; lines: LogLine[] } | undefined
  for (const line of lines) {
    if (line.kind === "endgroup") {
      open = undefined
    } else if (line.kind === "group") {
      open = { type: "group", line, lines: [] }
      rows.push(open)
    } else if (open) {
      open.lines.push(line)
    } else {
      rows.push({ type: "line", line })
    }
  }
  return rows
}

// The number of the group line holding line n, or undefined when n is not
// inside a group (a group line itself is always shown).
export function containingGroup(lines: LogLine[], n: number): number | undefined {
  let group: number | undefined
  for (const line of lines) {
    if (line.n === n) return line.kind === "group" ? undefined : group
    if (line.kind === "group") group = line.n
    else if (line.kind === "endgroup") group = undefined
  }
  return undefined
}

// The lines to render, as indexes [start, end): the last LOG_WINDOW lines,
// or, when line keep lies above them, LOG_WINDOW lines centred on it so a
// match near the top of a long step does not bring back every line below.
export function logWindow(lines: LogLine[], keep?: number): { start: number; end: number } {
  const cut = Math.max(0, lines.length - LOG_WINDOW)
  const at = keep === undefined ? -1 : lines.findIndex((l) => l.n === keep)
  if (at < 0 || at >= cut) return { start: cut, end: lines.length }
  const start = Math.max(0, at - LOG_WINDOW / 2)
  return { start, end: start + LOG_WINDOW }
}

export interface LogMatch {
  step: number
  line: number
  // which occurrence within the line, counting from 0
  index: number
}

// Case-insensitive matches over the text every shown line displays, ANSI
// codes removed, in step and line order.
export function findMatches(log: JobLog, query: string): LogMatch[] {
  if (!query) return []
  const out: LogMatch[] = []
  for (const step of log.steps) {
    for (const line of step.lines) {
      if (line.kind === "endgroup") continue
      matchRanges(parsedLine(line).text, query).forEach((_, index) => out.push({ step: step.number, line: line.n, index }))
    }
  }
  return out
}
