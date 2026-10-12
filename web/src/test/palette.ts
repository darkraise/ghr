export type Role =
  | "ground" | "surface" | "topBar" | "sunk" | "line" | "text" | "muted" | "accent" | "onAccent" | "tabLine"
  | "succeeded" | "failed" | "running" | "waiting"

// The Forge palette. ghr-theme.css carries the same colours as HSL triplets;
// palette.test.ts keeps the two in step.
export const PALETTE: Record<Role, { light: string; dark: string }> = {
  ground: { light: "#f6f8fa", dark: "#0d1117" },
  surface: { light: "#ffffff", dark: "#151b23" },
  topBar: { light: "#ffffff", dark: "#010409" },
  sunk: { light: "#eaeef2", dark: "#262c36" },
  line: { light: "#d1d9e0", dark: "#3d444d" },
  text: { light: "#1f2328", dark: "#f0f6fc" },
  muted: { light: "#59636e", dark: "#9198a1" },
  accent: { light: "#0969da", dark: "#4493f8" },
  onAccent: { light: "#ffffff", dark: "#0d1117" },
  tabLine: { light: "#fd8c73", dark: "#f78166" },
  succeeded: { light: "#1a7f37", dark: "#3fb950" },
  failed: { light: "#cf222e", dark: "#f85149" },
  running: { light: "#9a6700", dark: "#d29922" },
  waiting: { light: "#bc4c00", dark: "#db6d28" },
}

export const PILL_ALPHA = 0.15

// The accent's share in --color-series, the rest being sunk.
export const SERIES_MIX = 0.7

// The accent's share in --color-branch, the rest being surface.
export const BRANCH_MIX = 0.1

function rgb(hex: string): [number, number, number] {
  const n = Number.parseInt(hex.slice(1), 16)
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255]
}

function luminance(hex: string): number {
  const [r, g, b] = rgb(hex).map((v) => {
    const c = v / 255
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  }) as [number, number, number]
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

export function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number]
  return (hi + 0.05) / (lo + 0.05)
}

// fg painted over bg at the given opacity.
export function over(fg: string, bg: string, alpha: number): string {
  const f = rgb(fg)
  const b = rgb(bg)
  const mix = f.map((v, i) => Math.round(v * alpha + (b[i] ?? 0) * (1 - alpha)))
  return `#${mix.map((v) => v.toString(16).padStart(2, "0")).join("")}`
}

export function hslTriplet(hex: string): string {
  const [r, g, b] = rgb(hex).map((v) => v / 255) as [number, number, number]
  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  const l = (max + min) / 2
  const d = max - min
  let h = 0
  let s = 0
  if (d !== 0) {
    s = d / (1 - Math.abs(2 * l - 1))
    if (max === r) h = ((g - b) / d) % 6
    else if (max === g) h = (b - r) / d + 2
    else h = (r - g) / d + 4
    h *= 60
    if (h < 0) h += 360
  }
  return `${h.toFixed(1)} ${(s * 100).toFixed(1)}% ${(l * 100).toFixed(1)}%`
}
