export const OS_ARCH: ReadonlySet<string> = new Set(["linux", "windows", "macos", "x64", "x86", "arm", "arm64"])

// The daemon's own predicate (sched.MatchLabels).
export function matchLabels(jobLabels: string[], effective: string[]): boolean {
  const set = new Set(effective.map((l) => l.toLowerCase()))
  return jobLabels.every((l) => set.has(l.toLowerCase()))
}

// A group that asks for self-hosted but misses labels is one ghr could take;
// any other mismatch is a job meant for another runner.
export function classify(labels: string[], effective: string[]): { kind: "matched" | "unmatched" | "other"; missing: string[] } {
  if (labels.length === 0) return { kind: "other", missing: [] }
  if (matchLabels(labels, effective)) return { kind: "matched", missing: [] }
  const has = new Set(effective.map((l) => l.toLowerCase()))
  if (!labels.includes("self-hosted")) return { kind: "other", missing: [] }
  return { kind: "unmatched", missing: labels.filter((l) => !has.has(l)) }
}
