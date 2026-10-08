import { readdirSync, readFileSync } from "node:fs"
import { dirname, join, relative, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import ts from "typescript"
import { describe, expect, it } from "vitest"

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..")
const testHelpers = resolve(root, "src", "test")

// Every source file under src, plus the theme script. Tests and the test
// helpers hold fixture copy rather than UI, so they are left out.
function sourceFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name)
    if (entry.isDirectory()) return path === testHelpers ? [] : sourceFiles(path)
    return /\.(tsx?|css)$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name) ? [path] : []
  })
}

const FILES = [...sourceFiles(resolve(root, "src")).map((p) => relative(root, p).split("\\").join("/")), "public/theme-init.js"].sort()

const DASH = /[–—]/

function dashLiterals(name: string, text: string): string[] {
  const kind = name.endsWith(".tsx") ? ts.ScriptKind.TSX : name.endsWith(".js") ? ts.ScriptKind.JS : ts.ScriptKind.TS
  const source = ts.createSourceFile(name, text, ts.ScriptTarget.Latest, true, kind)
  const found: string[] = []
  const visit = (node: ts.Node) => {
    const literal =
      ts.isStringLiteral(node) ||
      ts.isNoSubstitutionTemplateLiteral(node) ||
      ts.isTemplateHead(node) ||
      ts.isTemplateMiddle(node) ||
      ts.isTemplateTail(node) ||
      ts.isJsxText(node)
    if (literal && DASH.test(node.text)) found.push(node.text.trim())
    ts.forEachChild(node, visit)
  }
  visit(source)
  return found
}

function gradientArgs(text: string, start: number): string[] {
  const args: string[] = []
  let depth = 0
  let current = ""
  for (let i = start; i < text.length; i++) {
    const ch = text.charAt(i)
    if (ch === "(") depth++
    if (ch === ")") {
      if (depth === 0) break
      depth--
    }
    if (ch === "," && depth === 0) {
      args.push(current.trim())
      current = ""
      continue
    }
    current += ch
  }
  args.push(current.trim())
  return args
}

// Tailwind 4 names its gradients bg-linear-*, bg-radial and bg-conic (with
// or without a suffix); Tailwind 3 named them bg-gradient-to-*.
const GRADIENT_UTILITY = /\bbg-(?:linear-|gradient-to-|radial(?:-|\b)|conic(?:-|\b))/

function styleViolations(text: string): string[] {
  const found: string[] = []
  if (/backdrop-filter|backdrop-blur-/.test(text)) found.push("backdrop blur")
  if (/background-clip:\s*text|bg-clip-text/.test(text)) found.push("background-clip: text")
  if (GRADIENT_UTILITY.test(text)) found.push("gradient utility")
  for (const m of text.matchAll(/(?:linear|radial)-gradient\(/g)) {
    const args = gradientArgs(text, (m.index ?? 0) + m[0].length)
    const stops = args.filter((a, i) => !(i === 0 && /^(to |at |circle|ellipse|closest|farthest|[-\d.]+(deg|turn|rad|grad))/.test(a)))
    if (stops.length > 1) found.push(`${m[0]}${args.join(", ")})`)
  }
  return found
}

// TagField's removable chips are controls, so they keep the kit's Badge.
const BADGE_ALLOWED = "src/components/tag-field.tsx"
const RETIRED = /darkraise-ui\/components\/badge|@\/components\/(?:glyph|state-badge)["/]/

describe("anti-slop rules", () => {
  it("catch what they are meant to catch", () => {
    expect(dashLiterals("x.tsx", 'const a = "one — two"; const b = <p>three – four</p>; const c = `five ${a} — six`')).toHaveLength(3)
    expect(dashLiterals("x.ts", "// a comment — is fine\nconst a = 1")).toEqual([])
    expect(styleViolations("background: linear-gradient(to right, red, blue)")).toHaveLength(1)
    expect(styleViolations("background: radial-gradient(circle, hsl(0 0% 0%), transparent)")).toHaveLength(1)
    expect(styleViolations("background: linear-gradient(red)")).toEqual([])
    expect(styleViolations('className="backdrop-blur-sm"')).toHaveLength(1)
    expect(styleViolations('className="bg-clip-text"')).toHaveLength(1)
    expect(styleViolations('className="bg-linear-to-r from-primary to-card"')).toHaveLength(1)
    expect(styleViolations('className="bg-gradient-to-b from-card"')).toHaveLength(1)
    expect(styleViolations('className="bg-radial from-primary"')).toHaveLength(1)
    expect(styleViolations('className="bg-radial-[at_25%_25%]"')).toHaveLength(1)
    expect(styleViolations('className="bg-conic-180 from-primary"')).toHaveLength(1)
    expect(styleViolations('className="bg-card bg-muted bg-primary/45"')).toEqual([])
    expect(RETIRED.test('import { Badge } from "darkraise-ui/components/badge"')).toBe(true)
    expect(RETIRED.test('import { StateBadge } from "@/components/state-badge"')).toBe(true)
    expect(RETIRED.test('import { Glyph } from "@/components/glyph"')).toBe(true)
    expect(RETIRED.test('import { ResultIcon } from "@/components/result-icon"')).toBe(false)
  })

  it("walk every source file and leave tests and test helpers out", () => {
    expect(FILES).toContain("public/theme-init.js")
    expect(FILES).toContain("src/pages/settings.tsx")
    expect(FILES).toContain("src/styles/ghr-theme.css")
    expect(FILES.filter((f) => f.includes(".test."))).toEqual([])
    expect(FILES.filter((f) => f.startsWith("src/test/"))).toEqual([])
  })

  it("keep the kit's Badge to TagField and import no Glyph or StateBadge", () => {
    const offenders = FILES.filter((file) => file !== BADGE_ALLOWED && RETIRED.test(readFileSync(resolve(root, file), "utf8")))
    expect(offenders).toEqual([])
  })

  it.each(FILES)("%s follows them", (file) => {
    const text = readFileSync(resolve(root, file), "utf8")
    const dashes = file.endsWith(".css") ? (text.match(/"[^"\n]*[–—][^"\n]*"/g) ?? []) : dashLiterals(file, text)
    expect(dashes).toEqual([])
    expect(styleViolations(text)).toEqual([])
  })
})
