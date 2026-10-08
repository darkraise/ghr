import { describe, expect, it } from "vitest"
import type { Storage, Toolchain } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { cacheParts, groupByTool, lastDotnetMajor, queueText, toolchainsSummary } from "./toolchains"

const tc = (tool: string, version: string, bytes = 1): Toolchain => ({ tool, version, arch: "x64", path: "", bytes, installed_at: "2026-10-01T00:00:00Z" })
const sdk = (version: string) => tc("dotnet", version)

describe("lastDotnetMajor", () => {
  it("names the major only for the last SDK of that major", () => {
    const all = [sdk("8.0.404"), sdk("8.0.100"), sdk("10.0.100")]
    expect(lastDotnetMajor(sdk("10.0.100"), all)).toBe("10")
    expect(lastDotnetMajor(sdk("8.0.404"), all)).toBe("")
    expect(lastDotnetMajor(tc("node", "22.11.0"), all)).toBe("")
  })
})

describe("queueText", () => {
  it("names the running operation, its progress and what waits after it", () => {
    expect(queueText(fixtures.storage.operations)).toBe("Installing node 24, extracting, 1 more queued")
    expect(queueText({ current: { id: "x", kind: "clear", target: "nuget", started_at: "" }, queued: 0, recent: [] })).toBe("Clearing nuget")
    expect(queueText({ current: { id: "x", kind: "prune", target: "all", started_at: "" }, queued: 3, recent: [] })).toBe("Prune all, 3 more queued")
    expect(queueText({ current: null, queued: 2, recent: [] })).toBe("2 queued")
    expect(queueText({ current: null, queued: 0, recent: [] })).toBe("")
  })
})

describe("groupByTool", () => {
  it("keeps each tool's versions together, tools in first-seen order", () => {
    const groups = groupByTool([tc("node", "22"), tc("python", "3.13"), tc("node", "24")])
    expect(groups.map((g) => [g.tool, g.rows.map((r) => r.version)])).toEqual([
      ["node", ["22", "24"]],
      ["python", ["3.13"]],
    ])
  })
})

describe("cacheParts", () => {
  it("gives the four largest tools a slot each and the rest, with the job folders, to Other", () => {
    const storage: Storage = {
      ...fixtures.storage,
      toolchains: [tc("node", "22", 500), tc("node", "24", 400), tc("go", "1.23", 300), tc("java", "21", 200), tc("python", "3.13", 100), tc("dotnet", "8.0", 50)],
      other_tool_cache: [{ name: "PyPy", bytes: 25 }],
    }
    expect(cacheParts(storage)).toEqual([
      { key: "node", label: "node", bytes: 900, slot: 1 },
      { key: "go", label: "go", bytes: 300, slot: 2 },
      { key: "java", label: "java", bytes: 200, slot: 3 },
      { key: "python", label: "python", bytes: 100, slot: 4 },
      { key: "__other", label: "Other", bytes: 75, slot: 5 },
    ])
  })

  it("is empty for an empty cache", () => {
    expect(cacheParts({ ...fixtures.storage, toolchains: null, other_tool_cache: null })).toEqual([])
  })
})

describe("toolchainsSummary", () => {
  it("counts the managed toolchains and sizes the whole cache", () => {
    expect(toolchainsSummary(fixtures.storage)).toBe("1 installed, 270.0 MB")
    expect(toolchainsSummary({ ...fixtures.storage, measured_at: undefined })).toBe("Not measured yet")
  })
})
