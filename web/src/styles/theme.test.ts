import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { generateTokens } from "darkraise-ui/theme"
import { describe, expect, it } from "vitest"
import { themeConfig } from "@/theme.config"

const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "ghr-theme.css"), "utf8")

function block(selector: string): Record<string, string> {
  const at = css.indexOf(`${selector} {`)
  if (at < 0) throw new Error(`ghr-theme.css has no "${selector} {" block`)
  const body = css.slice(css.indexOf("{", at) + 1, css.indexOf("}", at))
  const out: Record<string, string> = {}
  for (const m of body.matchAll(/(--[\w-]+):\s*([^;]+);/g)) out[m[1] ?? ""] = (m[2] ?? "").trim()
  return out
}

function tokens(mode: "dark" | "light"): Record<string, string> {
  return { ...block(":root"), ...block(`:root[data-mode="${mode}"]`) }
}

function rgb(value: string): number[] {
  const [h = 0, s = 0, l = 0] = value.replace("!important", "").trim().split(/\s+/).map((p) => parseFloat(p))
  const a = (s / 100) * Math.min(l / 100, 1 - l / 100)
  const f = (n: number) => {
    const k = (n + h / 30) % 12
    return l / 100 - a * Math.max(-1, Math.min(k - 3, 9 - k, 1))
  }
  return [f(0), f(8), f(4)]
}

function luminance(value: string): number {
  const [r = 0, g = 0, b = 0] = rgb(value).map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4))
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrast(a: string | undefined, b: string | undefined): number {
  const x = luminance(a ?? "")
  const y = luminance(b ?? "")
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05)
}

const PAIRS: [string, string, number][] = [
  ["--foreground", "--background", 4.5],
  ["--foreground", "--card", 4.5],
  ["--muted-foreground", "--background", 4.5],
  ["--muted-foreground", "--card", 4.5],
  ["--muted-foreground", "--muted", 4.5],
  ["--primary", "--background", 4.5],
  ["--primary", "--card", 4.5],
  ["--success", "--background", 4.5],
  ["--success", "--card", 4.5],
  ["--destructive", "--background", 4.5],
  ["--destructive", "--card", 4.5],
  ["--warning", "--background", 4.5],
  ["--warning", "--card", 4.5],
  ["--primary-foreground", "--primary", 4.5],
  ["--success-foreground", "--success", 4.5],
  ["--destructive-foreground", "--destructive", 4.5],
  ["--warning-foreground", "--warning", 4.5],
  ["--sidebar-foreground", "--surface-sidebar", 4.5],
  ["--sidebar-foreground", "--sidebar-hover-bg", 4.5],
  ["--sidebar-foreground-muted", "--surface-sidebar", 4.5],
  ["--sidebar-foreground-muted", "--sidebar-hover-bg", 4.5],
  ["--primary", "--muted", 3],
]

describe("ghr palette", () => {
  it.each(["dark", "light"] as const)("overrides every token the engine writes in %s mode", (mode) => {
    const d = themeConfig.defaults
    const engine = generateTokens({
      accentColor: d.accentColor,
      surfaceColor: d.surfaceColor,
      preset: d.preset,
      backgroundStyle: d.backgroundStyle,
      backgroundIntensity: d.backgroundIntensity,
      accentIntensity: d.accentIntensity,
      mode,
    })
    const ours = tokens(mode)
    expect(Object.keys(engine).filter((k) => !(k in ours))).toEqual([])
    expect(Object.keys(engine).filter((k) => !ours[k]?.endsWith("!important"))).toEqual([])
  })

  it.each(["dark", "light"] as const)("keeps text and outlines readable in %s mode", (mode) => {
    const t = tokens(mode)
    for (const [fg, bg, min] of PAIRS) expect(contrast(t[fg], t[bg]), `${fg} on ${bg}`).toBeGreaterThanOrEqual(min)
  })

  it("neutralises shadows, noise and gradients", () => {
    const root = block(":root")
    expect(root["--shadow-card"]).toBe("none !important")
    expect(root["--shadow-dropdown"]).toBe("none !important")
    expect(root["--noise-opacity"]).toBe("0 !important")
    expect(root["--content-gradient-overlay"]).toBe("none !important")
  })
})

const LOG_COLOURS = ["black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"].flatMap((c) => [
  `--log-ansi-${c}`,
  `--log-ansi-bright-${c}`,
])

describe("log palette", () => {
  it.each(["dark", "light"] as const)("is dark and readable in %s mode", (mode) => {
    const t = tokens(mode)
    expect(luminance(t["--log-bg"] ?? "")).toBeLessThan(0.02)
    for (const fg of ["--log-fg", "--log-muted", "--log-command", ...LOG_COLOURS]) {
      expect(contrast(t[fg], t["--log-bg"]), `${fg} on --log-bg`).toBeGreaterThanOrEqual(4.5)
      expect(contrast(t[fg], t["--log-target-bg"]), `${fg} on --log-target-bg`).toBeGreaterThanOrEqual(4.5)
    }
    expect(contrast(t["--log-error-fg"], t["--log-error-bg"])).toBeGreaterThanOrEqual(4.5)
    expect(contrast(t["--log-warning-fg"], t["--log-warning-bg"])).toBeGreaterThanOrEqual(4.5)
    expect(contrast(t["--log-fg"], t["--log-match-bg"])).toBeGreaterThanOrEqual(4.5)
    expect(contrast(t["--log-current"], t["--log-bg"])).toBeGreaterThanOrEqual(3)
  })
})

describe("ghr chrome styles", () => {
  it("paints the log panel dark and marks error and warning lines on their left edge", () => {
    expect(css).toMatch(/\.ghr-log-panel \{\s*background: hsl\(var\(--log-bg\)\);\s*color: hsl\(var\(--log-fg\)\);/)
    expect(css).toMatch(/\.ghr-log-error \{[^}]*box-shadow: inset 2px 0 0 hsl\(var\(--destructive\)\);/)
    expect(css).toMatch(/\.ghr-log-warning \{[^}]*box-shadow: inset 2px 0 0 hsl\(var\(--warning\)\);/)
  })

  it("keeps the light-mode log panel readable with the dark palette", () => {
    const light = tokens("light")
    const panel = { ...light, ...block(':root[data-mode="light"] .ghr-log-panel') }
    const checks: [string, number][] = [
      ["--foreground", 4.5],
      ["--muted-foreground", 4.5],
      ["--warning", 4.5],
      ["--destructive", 4.5],
      ["--success", 4.5],
      ["--running", 4.5],
      ["--primary", 3],
    ]
    for (const [fg, min] of checks) {
      expect(contrast(panel[fg], light["--log-bg"]), `${fg} on the log`).toBeGreaterThanOrEqual(min)
      expect(contrast(panel[fg], panel["--card"]), `${fg} on a panel card`).toBeGreaterThanOrEqual(min)
    }
  })

  it("recomputes the kit's surface fills inside the log panel", () => {
    // bg-card paints var(--surface-card-fill), which the kit computes on
    // <html>; without these the panel's cards stay the light-mode white.
    const rail = block(':root[data-mode="light"] .ghr-log-panel')
    expect(rail["--surface-card-fill"]).toBe("hsl(var(--card))")
    expect(rail["--surface-popover-fill"]).toBe("hsl(var(--popover))")
    expect(rail["--surface-raised-fill"]).toBe("hsl(var(--surface-raised))")
    expect(rail["--surface-overlay-fill"]).toBe("hsl(var(--surface-overlay))")
    expect(rail["--control-surface"]).toBe("var(--card)")
    expect(rail["--control-well"]).toBe("var(--card)")
  })

  it("pins one radius: 6px on controls and on surfaces", () => {
    expect(block(":root[data-radius]")).toEqual({ "--radius": "6px", "--radius-button": "6px" })
    expect(css).toMatch(/--radius-md: 6px;/)
    expect(css).toMatch(/--radius-lg: 6px;/)
    expect(css).toMatch(/--radius-xl: 6px;/)
  })

  it("stops the pulse and the spinner when motion is reduced", () => {
    expect(css).toMatch(/\.ghr-pulse \{\s*animation: ghr-pulse 1\.8s ease-out infinite;/)
    expect(css).toMatch(/@media \(prefers-reduced-motion: reduce\) \{\s*\.ghr-pulse \{\s*animation: none;\s*\}\s*\.dr-spinner-circle \{\s*animation: none;/)
  })

  it("lets the document scroll", () => {
    expect(css).toMatch(/html,\s*body \{\s*height: auto;\s*overflow: visible;\s*\}/)
  })

  it("draws every Lucide icon at one stroke width", () => {
    expect(css).toMatch(/\.lucide \{\s*stroke-width: 1\.75;/)
  })
})

describe("theme config", () => {
  it("pins every axis and turns the switcher off", () => {
    expect(themeConfig.switcher.enabled).toBe(false)
    expect(Object.values(themeConfig.switcher.axes).every((on) => !on)).toBe(true)
    expect(themeConfig.defaults).toMatchObject({
      preset: "default",
      density: "compact",
      fontSize: "large",
      elevation: "low",
      buttonElevation: "flat",
      radius: "subtle",
      controlDepth: "flush",
      shellStyle: "classic",
      backgroundStyle: "solid",
      outerGlow: "none",
      innerGlow: "none",
      mode: "system",
    })
  })
})
