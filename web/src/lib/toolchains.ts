import type { Operations, Toolchain } from "@/api/types"

const VERBS: Record<string, string> = { install: "installing", remove: "removing", clear: "clearing" }

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
  let text = `${VERBS[c.kind] ?? c.kind} ${c.target}`
  if (c.progress) text += ` — ${c.progress}`
  if (ops.queued > 0) text += ` (${ops.queued} queued)`
  return text
}
