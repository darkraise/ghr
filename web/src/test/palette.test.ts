import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { describe, expect, it } from "vitest"
import { BRANCH_MIX, contrast, hslTriplet, over, PALETTE, PILL_ALPHA, SERIES_MIX, type Role } from "./palette"

const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "../styles/ghr-theme.css"), "utf8")
const MODES = ["light", "dark"] as const

function block(mode: string): string {
  const start = css.indexOf(`:root[data-mode="${mode}"] {`)
  return css.slice(start, css.indexOf("\n}", start))
}

function token(mode: string, name: string): string | undefined {
  return new RegExp(`\\n\\s*${name}:\\s*([^;!]+?)\\s*(?:!important)?;`).exec(block(mode))?.[1]
}

const TOKENS: [string, Role][] = [
  ["--background", "ground"],
  ["--card", "surface"],
  ["--muted", "sunk"],
  ["--border", "line"],
  ["--foreground", "text"],
  ["--muted-foreground", "muted"],
  ["--primary", "accent"],
  ["--primary-foreground", "onAccent"],
  ["--success", "succeeded"],
  ["--destructive", "failed"],
  ["--warning", "waiting"],
  ["--running", "running"],
  ["--top-bar", "topBar"],
  ["--tab-line", "tabLine"],
]

describe("the Forge palette", () => {
  it.each(MODES)("is what ghr-theme.css sets in %s mode", (mode) => {
    for (const [name, role] of TOKENS) expect([name, token(mode, name)]).toEqual([name, hslTriplet(PALETTE[role][mode])])
  })

  it.each(MODES)("keeps text and status colours at 4.5:1 on boxes and on the ground in %s mode", (mode) => {
    const roles: Role[] = ["text", "muted", "accent", "succeeded", "failed", "running", "waiting"]
    for (const role of roles) {
      for (const bg of ["surface", "ground"] as const) {
        expect([role, bg, contrast(PALETTE[role][mode], PALETTE[bg][mode]) >= 4.5]).toEqual([role, bg, true])
      }
    }
  })

  it.each(MODES)("keeps a pill's icon at 3:1 on its own tint in %s mode", (mode) => {
    const roles: Role[] = ["accent", "succeeded", "failed", "running", "waiting", "muted"]
    for (const role of roles) {
      for (const bg of ["surface", "ground"] as const) {
        const tint = over(PALETTE[role][mode], PALETTE[bg][mode], PILL_ALPHA)
        expect([role, bg, contrast(PALETTE[role][mode], tint) >= 3]).toEqual([role, bg, true])
        expect([role, bg, contrast(PALETTE.text[mode], tint) >= 4.5]).toEqual([role, bg, true])
      }
    }
  })

  it("mixes the accent toward sunk for a series that is not a state", () => {
    expect(css).toContain(`--color-series: color-mix(in srgb, hsl(var(--primary)) ${SERIES_MIX * 100}%, hsl(var(--muted)));`)
    for (const mode of MODES) {
      const series = over(PALETTE.accent[mode], PALETTE.sunk[mode], SERIES_MIX)
      const track = over(PALETTE.sunk[mode], PALETTE.surface[mode], 0.3)
      expect([mode, "surface", contrast(series, PALETTE.surface[mode]) >= 3]).toEqual([mode, "surface", true])
      expect([mode, "track", contrast(series, track) >= 3]).toEqual([mode, "track", true])
    }
  })

  it.each(MODES)("keeps the branch label's accent text at 4.5:1 on its own fill in %s mode", (mode) => {
    // The fill is mixed with the box colour, not translucent: a hovered row's
    // wash under a translucent fill takes the text below 4.5:1.
    expect(css).toContain(`--color-branch: color-mix(in srgb, hsl(var(--primary)) ${BRANCH_MIX * 100}%, hsl(var(--card)));`)
    const fill = over(PALETTE.accent[mode], PALETTE.surface[mode], BRANCH_MIX)
    expect(contrast(PALETTE.accent[mode], fill)).toBeGreaterThanOrEqual(4.5)
  })

  it("computes contrast, blends and triplets exactly", () => {
    expect(contrast("#000000", "#ffffff")).toBeCloseTo(21, 5)
    expect(contrast("#777777", "#777777")).toBe(1)
    expect(contrast("#ffffff", "#767676")).toBeCloseTo(4.54, 2)
    expect(over("#000000", "#ffffff", 0.5)).toBe("#808080")
    expect(over("#ff0000", "#0000ff", 1)).toBe("#ff0000")
    expect(hslTriplet("#ff0000")).toBe("0.0 100.0% 50.0%")
    expect(hslTriplet("#ffffff")).toBe("0.0 0.0% 100.0%")
    expect(hslTriplet("#0969da")).toBe("212.4 92.1% 44.5%")
  })

  it("puts readable text on the primary button", () => {
    for (const mode of MODES) expect(contrast(PALETTE.onAccent[mode], PALETTE.accent[mode])).toBeGreaterThanOrEqual(4.5)
  })
})
