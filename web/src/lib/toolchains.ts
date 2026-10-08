import type { Operations, Storage, Toolchain } from "@/api/types"
import { humanBytes } from "./format"

const VERBS: Record<string, string> = { install: "Installing", remove: "Removing", clear: "Clearing" }

const sentence = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

// Removing the last SDK of a .NET major also removes that major's runtimes
// and packs, so the confirmation says so.
export function lastDotnetMajor(t: Toolchain, all: Toolchain[]): string {
  if (t.tool !== "dotnet") return ""
  const major = t.version.split(".")[0] ?? ""
  return all.some((o) => o.tool === "dotnet" && o.version !== t.version && o.version.startsWith(`${major}.`)) ? "" : major
}

export function queueText(ops: Operations): string {
  const c = ops.current
  if (!c) return ops.queued > 0 ? `${ops.queued} queued` : ""
  let text = `${VERBS[c.kind] ?? sentence(c.kind)} ${c.target}`
  if (c.progress) text += `, ${c.progress}`
  if (ops.queued > 0) text += `, ${ops.queued} more queued`
  return text
}

export interface ToolGroup {
  tool: string
  rows: Toolchain[]
}

export function groupByTool(toolchains: Toolchain[]): ToolGroup[] {
  const groups: ToolGroup[] = []
  for (const t of toolchains) {
    const group = groups.find((g) => g.tool === t.tool)
    if (group) group.rows.push(t)
    else groups.push({ tool: t.tool, rows: [t] })
  }
  return groups
}

export interface CachePart {
  key: string
  label: string
  bytes: number
  slot: 1 | 2 | 3 | 4 | 5
}

const SLOTS = [1, 2, 3, 4] as const

// The four largest tools get a chart colour each; smaller tools and the
// folders jobs installed share the fifth as "Other". A tool is not a status,
// so the bar never uses a status colour.
export function cacheParts(storage: Storage): CachePart[] {
  const totals = new Map<string, number>()
  for (const t of storage.toolchains ?? []) totals.set(t.tool, (totals.get(t.tool) ?? 0) + t.bytes)
  const tools = [...totals].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
  const parts: CachePart[] = tools.slice(0, 4).map(([tool, bytes], i) => ({ key: tool, label: tool, bytes, slot: SLOTS[i] ?? 4 }))
  const rest = tools.slice(4).reduce((n, [, bytes]) => n + bytes, 0) + (storage.other_tool_cache ?? []).reduce((n, f) => n + f.bytes, 0)
  if (rest > 0) parts.push({ key: "__other", label: "Other", bytes: rest, slot: 5 })
  return parts
}

export function toolchainsSummary(storage: Storage): string {
  if (!storage.measured_at) return "Not measured yet"
  const tools = storage.toolchains ?? []
  const bytes = [...tools, ...(storage.other_tool_cache ?? [])].reduce((n, t) => n + t.bytes, 0)
  return `${tools.length} installed, ${humanBytes(bytes)}`
}
