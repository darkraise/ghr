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
  return n === 1 ? "● 1 unsaved change" : `● ${n} unsaved changes`
}
