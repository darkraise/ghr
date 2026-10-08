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

  it("neutralises shadows, noise, gradients and the kit's overlays", () => {
    const root = block(":root")
    expect(root["--shadow-card"]).toBe("none !important")
    expect(root["--shadow-dropdown"]).toBe("none !important")
    expect(root["--noise-opacity"]).toBe("0 !important")
    expect(root["--content-gradient-overlay"]).toBe("none !important")
    expect(css).toMatch(/\.sidebar-gradient-overlay::before,\s*\.header-gradient-overlay::before,\s*main\[data-content\]::before \{\s*background: none !important;/)
  })
})
