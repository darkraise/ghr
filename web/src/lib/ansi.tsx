import type { ReactNode } from "react"

export interface AnsiSegment {
  text: string
  // "red" or "bright-red"; absent for the default colour.
  color?: string
  bold: boolean
}

const COLORS = ["black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"]

// One alternative per thing the tokenizer acts on: a CSI sequence (SGR when
// its final byte is "m"), an OSC sequence ended by BEL, ST or the end of the
// line, any other escape, DEL, the C1 controls, and the C0 controls other
// than tab.
// eslint-disable-next-line no-control-regex -- log lines carry raw control bytes
const TOKEN = /\x1b\[([0-?]*)[ -/]*([@-~])|\x1b\][\s\S]*?(?:\x07|\x1b\\|$)|\x1b[ -/]*[0-~]?|[\x00-\x08\x0a-\x1f\x7f-\x9f]/g

// A line rewritten with carriage returns (a progress bar) shows its last
// state; a trailing CR from a CRLF line ending is not a rewrite.
function lastSegment(raw: string): string {
  const line = raw.replace(/\r+$/, "")
  return line.slice(line.lastIndexOf("\r") + 1)
}

function applySgr(params: string, style: { color?: string; bold: boolean }) {
  const codes = params === "" ? [0] : params.split(";").map((p) => (p === "" ? 0 : Number(p)))
  for (let i = 0; i < codes.length; i++) {
    const c = codes[i] ?? 0
    if (c === 0) {
      style.color = undefined
      style.bold = false
    } else if (c === 1) style.bold = true
    else if (c === 22) style.bold = false
    else if (c === 39) style.color = undefined
    else if (c >= 30 && c <= 37) style.color = COLORS[c - 30]
    else if (c >= 90 && c <= 97) style.color = `bright-${COLORS[c - 90]}`
    else if (c === 38 || c === 48) {
      // 256-colour and true-colour arguments are skipped, not read as codes.
      if (codes[i + 1] === 5) i += 2
      else if (codes[i + 1] === 2) i += 4
    }
  }
}

export function ansiSegments(raw: string): AnsiSegment[] {
  const line = lastSegment(raw)
  const out: AnsiSegment[] = []
  const style: { color?: string; bold: boolean } = { bold: false }
  const push = (text: string) => {
    if (!text) return
    const last = out[out.length - 1]
    if (last && last.color === style.color && last.bold === style.bold) last.text += text
    else out.push({ text, color: style.color, bold: style.bold })
  }
  let at = 0
  for (const m of line.matchAll(TOKEN)) {
    push(line.slice(at, m.index))
    at = m.index + m[0].length
    if (m[2] === "m") applySgr(m[1] ?? "", style)
  }
  push(line.slice(at))
  return out
}

export function plainText(raw: string): string {
  return ansiSegments(raw)
    .map((s) => s.text)
    .join("")
}

export interface ParsedLine {
  segments: AnsiSegment[]
  // The segments' text joined: what the line shows and what a search reads.
  text: string
}

const parsed = new WeakMap<object, ParsedLine>()

// Keyed by the line object, so a line is parsed once however often it is
// rendered or searched, and the entry goes when the log does.
export function parsedLine(line: { text: string }): ParsedLine {
  let out = parsed.get(line)
  if (!out) {
    const segments = ansiSegments(line.text)
    out = { segments, text: segments.map((s) => s.text).join("") }
    parsed.set(line, out)
  }
  return out
}

// Case-insensitive, non-overlapping occurrences of query in text, as
// [start, end) offsets in text itself (lowercasing a copy can change lengths).
export function matchRanges(text: string, query: string): [number, number][] {
  if (!query) return []
  const re = new RegExp(query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), "gi")
  return [...text.matchAll(re)].map((m) => [m.index, m.index + m[0].length])
}

function segmentClass(s: AnsiSegment): string | undefined {
  const names = [s.color ? `ghr-log-ansi-${s.color}` : "", s.bold ? "font-semibold" : ""].filter(Boolean)
  return names.length > 0 ? names.join(" ") : undefined
}

// Renders one log line as React elements: coloured and bold spans, and a
// <mark> on each occurrence of query, the one numbered current marked as the
// current match.
export function renderAnsi(line: { text: string }, query = "", current?: number): ReactNode[] {
  const { segments, text: plain } = parsedLine(line)
  const ranges = matchRanges(plain, query)
  const out: ReactNode[] = []
  let offset = 0
  let r = 0
  for (const s of segments) {
    const cls = segmentClass(s)
    let i = 0
    while (i < s.text.length) {
      const pos = offset + i
      while (r < ranges.length && (ranges[r]?.[1] ?? 0) <= pos) r++
      const range = ranges[r]
      const inside = range !== undefined && range[0] <= pos
      const end = Math.min(s.text.length, (inside ? range[1] : (range?.[0] ?? Infinity)) - offset)
      const text = s.text.slice(i, end)
      const key = out.length
      const piece = cls ? (
        <span key={key} className={cls}>
          {text}
        </span>
      ) : (
        text
      )
      out.push(
        inside ? (
          <mark key={key} className="ghr-log-match" data-current={r === current ? "true" : undefined}>
            {piece}
          </mark>
        ) : (
          piece
        ),
      )
      i = end
    }
    offset += s.text.length
  }
  return out
}
