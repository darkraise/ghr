export type Values = Record<string, unknown>
export type Equal = (key: string, a: unknown, b: unknown) => boolean

export function sameValue(a: unknown, b: unknown): boolean {
  if (Array.isArray(a) && Array.isArray(b)) return a.length === b.length && a.every((v, i) => v === b[i])
  return a === b
}

export const plainEqual: Equal = (_key, a, b) => sameValue(a, b)

export function changedKeys(draft: Values, loaded: Values, eq: Equal = plainEqual): string[] {
  return Object.keys(draft).filter((k) => !eq(k, draft[k], loaded[k]))
}

// An entry that holds the loaded value is no edit, and left in the draft it
// would count as one again once the loaded value moved. The stored values are
// compared, so text that only means the same (60s over 1m) stays as typed.
// Returns the draft itself when nothing is dropped.
export function prune(draft: Values, loaded: Values): Values {
  const kept = Object.entries(draft).filter(([k, v]) => !sameValue(v, loaded[k]))
  return kept.length === Object.keys(draft).length ? draft : Object.fromEntries(kept)
}

// After a save, an edit still equal to what was sent is settled; one made
// while the save was in flight stays pending.
export function settle(draft: Values, sent: Values, eq: Equal = plainEqual): Values {
  return Object.fromEntries(Object.entries(draft).filter(([k, v]) => !(k in sent) || !eq(k, v, sent[k])))
}

export function rejected(message: string): { lines: string[]; more: number } {
  const all = message.split("; ")
  return { lines: all.slice(0, 3), more: Math.max(0, all.length - 3) }
}

export function unsavedText(n: number): string {
  return n === 1 ? "1 unsaved change" : `${n} unsaved changes`
}
