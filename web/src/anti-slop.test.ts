import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import ts from "typescript"
import { describe, expect, it } from "vitest"

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..")

// The files the identity and Dashboard spec created or restyled. Spec 2 adds
// each page it redesigns, until this list is all of src.
const FILES = [
  "public/theme-init.js",
  "src/components/activity-panel.tsx",
  "src/components/add-repo-dialog.tsx",
  "src/components/brand.tsx",
  "src/components/buckets-chart.tsx",
  "src/components/capacity-bar.tsx",
  "src/components/disk-breakdown.tsx",
  "src/components/event-list.tsx",
  "src/components/install-dialog.tsx",
  "src/components/label-check-group.tsx",
  "src/components/lanes-chart.tsx",
  "src/components/log-view.tsx",
  "src/components/mode-control.tsx",
  "src/components/page/error-line.tsx",
  "src/components/page/field.tsx",
  "src/components/page/section-nav.tsx",
  "src/components/page/section.tsx",
  "src/components/page/split-view.tsx",
  "src/components/page/state-text.tsx",
  "src/components/prune-section.tsx",
  "src/components/refused-hint.tsx",
  "src/components/repo-activity-strip.tsx",
  "src/components/repo-activity.tsx",
  "src/components/repo-table.tsx",
  "src/components/runner-list.tsx",
  "src/components/runner-panel.tsx",
  "src/components/registrations-group.tsx",
  "src/components/result-icon.tsx",
  "src/components/runner-meter.tsx",
  "src/components/save-bar.tsx",
  "src/components/shell.tsx",
  "src/components/size-bar.tsx",
  "src/components/sparkline.tsx",
  "src/components/stat-card.tsx",
  "src/components/step-list.tsx",
  "src/components/stop-runner-dialog.tsx",
  "src/components/storage-disk.tsx",
  "src/components/storage-tables.tsx",
  "src/components/toolchain-actions.tsx",
  "src/components/toolchain-sections.tsx",
  "src/components/unsaved-guard.tsx",
  "src/components/update-card.tsx",
  "src/components/window-control.tsx",
  "src/api/client.ts",
  "src/api/hooks.ts",
  "src/lib/activity-view.ts",
  "src/lib/capacity.ts",
  "src/lib/disk.ts",
  "src/lib/draft.ts",
  "src/lib/format.ts",
  "src/lib/history.ts",
  "src/lib/repos.ts",
  "src/lib/status.ts",
  "src/lib/storage.ts",
  "src/lib/steps.ts",
  "src/lib/summary.ts",
  "src/lib/toolchains.ts",
  "src/lib/use-activity-window.ts",
  "src/lib/use-repo-actions.ts",
  "src/lib/use-media-query.ts",
  "src/lib/use-operation-toasts.ts",
  "src/lib/use-popular-set.ts",
  "src/lib/use-prune.ts",
  "src/lib/use-width.ts",
  "src/pages/dashboard.tsx",
  "src/pages/history.tsx",
  "src/router.tsx",
  "src/pages/login.tsx",
  "src/pages/repositories.tsx",
  "src/pages/repository.tsx",
  "src/pages/runners.tsx",
  "src/pages/setup.tsx",
  "src/pages/storage.tsx",
  "src/pages/toolchains.tsx",
  "src/query.ts",
  "src/styles/ghr-theme.css",
  "src/theme.config.ts",
]

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

function styleViolations(text: string): string[] {
  const found: string[] = []
  if (/backdrop-filter|backdrop-blur-/.test(text)) found.push("backdrop blur")
  if (/background-clip:\s*text|bg-clip-text/.test(text)) found.push("background-clip: text")
  for (const m of text.matchAll(/(?:linear|radial)-gradient\(/g)) {
    const args = gradientArgs(text, (m.index ?? 0) + m[0].length)
    const stops = args.filter((a, i) => !(i === 0 && /^(to |at |circle|ellipse|closest|farthest|[-\d.]+(deg|turn|rad|grad))/.test(a)))
    if (stops.length > 1) found.push(`${m[0]}${args.join(", ")})`)
  }
  return found
}

describe("anti-slop rules", () => {
  it("catch what they are meant to catch", () => {
    expect(dashLiterals("x.tsx", 'const a = "one — two"; const b = <p>three – four</p>; const c = `five ${a} — six`')).toHaveLength(3)
    expect(dashLiterals("x.ts", "// a comment — is fine\nconst a = 1")).toEqual([])
    expect(styleViolations("background: linear-gradient(to right, red, blue)")).toHaveLength(1)
    expect(styleViolations("background: radial-gradient(circle, hsl(0 0% 0%), transparent)")).toHaveLength(1)
    expect(styleViolations("background: linear-gradient(red)")).toEqual([])
    expect(styleViolations('className="backdrop-blur-sm"')).toHaveLength(1)
    expect(styleViolations('className="bg-clip-text"')).toHaveLength(1)
  })

  it.each(FILES)("%s follows them", (file) => {
    const text = readFileSync(resolve(root, file), "utf8")
    const dashes = file.endsWith(".css") ? (text.match(/"[^"\n]*[–—][^"\n]*"/g) ?? []) : dashLiterals(file, text)
    expect(dashes).toEqual([])
    expect(styleViolations(text)).toEqual([])
  })
})
