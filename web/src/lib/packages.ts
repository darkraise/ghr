import type { PackageInfo, Packages } from "@/api/types"

// The daemon's rule for a search term (packages.ValidTerm).
const TERM = /^[a-z0-9+.-]{2,64}$/

export function validTerm(term: string): boolean {
  return TERM.test(term)
}

// The daemon never marks a base package upgradable, so every row here is one
// "Upgrade all" upgrades.
export function upgradable(p?: Packages): PackageInfo[] {
  return (p?.managed ?? []).filter((i) => i.upgradable === true)
}

export function filterPackages(list: PackageInfo[] | null | undefined, filter: string): PackageInfo[] {
  const all = list ?? []
  const f = filter.trim().toLowerCase()
  if (f === "") return all
  return all.filter((p) => p.name.includes(f) || (p.description ?? "").toLowerCase().includes(f))
}
