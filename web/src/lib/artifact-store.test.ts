import { describe, expect, it } from "vitest"
import type { ArtifactStore, ArtifactStoreRepo } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { expiryText, LINK_TTLS, mergeDeletions, olderThan, parseArtifactsSearch, selfHostedSummary, stateLine, storeName } from "./artifact-store"

const now = Date.parse(fixtures.status.now)
const darkmem = fixtures.artifactStore.repos[0] as ArtifactStoreRepo
const repo = (over: Partial<ArtifactStoreRepo>): ArtifactStoreRepo => ({ ...darkmem, ...over })

describe("parseArtifactsSearch", () => {
  it.each<[Record<string, unknown>, unknown]>([
    [{ store: "self-hosted" }, { store: "self-hosted" }],
    [{ store: "github" }, {}],
    [{ store: 1 }, {}],
    [{}, {}],
  ])("reads %j as %j", (search, want) => {
    expect(parseArtifactsSearch(search)).toEqual(want)
  })
})

describe("stateLine", () => {
  it.each<[string, Partial<ArtifactStoreRepo>, string, string]>([
    ["on", { state: "on", enabled: true }, "On · uploads from ghr's runners go to RustFS", "ok"],
    ["off", { state: "off", enabled: false }, "Off · uploads go to GitHub", "off"],
    ["not served", { state: "not_served", enabled: false }, "Not served by ghr · only repositories ghr runs jobs for can use self-hosted storage", "off"],
    ["not served but still on", { state: "not_served", enabled: true }, "Not served by ghr · only repositories ghr runs jobs for can use self-hosted storage", "warn"],
    ["public", { state: "public", private: false, enabled: false }, "Public repositories stay on GitHub", "off"],
  ])("words %s", (_name, over, text, tone) => {
    expect(stateLine(repo(over))).toEqual({ text, tone })
  })
})

describe("storeName and LINK_TTLS", () => {
  it("names the stores and the link times", () => {
    expect([storeName("rustfs"), storeName(undefined), storeName("")]).toEqual(["RustFS", "GitHub", "GitHub"])
    expect(LINK_TTLS.map((t) => [t.ttl, t.label])).toEqual([
      ["1h", "1 hour"],
      ["24h", "24 hours"],
      ["7d", "7 days"],
    ])
  })
})

describe("mergeDeletions", () => {
  it("merges both stores' deletions newest first and names GitHub's store", () => {
    const merged = mergeDeletions(fixtures.githubStorage.deletions, fixtures.artifactStore.deletions)
    expect(merged.map((d) => [d.at, d.store, d.name])).toEqual([
      ["2026-10-03T13:58:00Z", "rustfs", "frontend-dist · run 90"],
      ["2026-10-03T13:56:00Z", "github", "coverage-old"],
      ["2026-10-03T12:05:00Z", "github", "node-cache-Linux-x64-npm-0b1c"],
      ["2026-10-03T11:05:00Z", "rustfs", "image-cache · run 70"],
    ])
  })
})

describe("olderThan", () => {
  it.each<[string, string[] | null]>([
    ["3d", ["darkraise/darkmem/95/frontend-dist.zip"]],
    ["1h", ["darkraise/darkmem/101/frontend-dist.zip", "darkraise/darkmem/95/frontend-dist.zip"]],
    [" 1d ", ["darkraise/darkmem/95/frontend-dist.zip"]],
    ["30d", []],
    ["soon", null],
    ["0", null],
    ["", null],
  ])("picks the items older than %j", (age, want) => {
    expect(olderThan(darkmem.items, age, now)?.map((i) => i.key) ?? null).toEqual(want)
  })
})

describe("expiryText", () => {
  it.each<[string, number, string, string]>([
    ["an item with its own expiry", 0, "3d", "2026-10-08"],
    ["an item the cleanup age removes", 1, "3d", "2026-10-06"],
    ["an item under a cleanup that is off", 1, "off", "Kept"],
  ])("gives %s", (_name, index, cleanup, want) => {
    const item = darkmem.items[index]
    if (!item) throw new Error(`no item ${index}`)
    expect(expiryText(item, cleanup)).toBe(want)
  })
})

describe("selfHostedSummary", () => {
  it("sums up the check, the cleanup and the repositories on", () => {
    expect(selfHostedSummary(fixtures.artifactStore, fixtures.config, now)).toBe("Checked 14:00 · cleanup 7d (1 repository override) · 2 repositories on")
  })

  it("reads an empty global age as the 7-day default and counts only served repositories that are on", () => {
    const store: ArtifactStore = {
      ...fixtures.artifactStore,
      checked_at: undefined,
      repos: fixtures.artifactStore.repos.map((r) => ({
        ...r,
        cleanup_source: "global",
        enabled: true,
        state: r.name === "darkmem" ? "on" : r.name === "old-docs" ? "public" : "not_served",
      })),
    }
    const config = { ...fixtures.config, artifact_store: { endpoint: "https://s3.example.sg", bucket: "gha-artifacts" } }
    expect(selfHostedSummary(store, config, now)).toBe("Not checked yet · cleanup 7d · 1 repository on")
  })

  it("says when nothing is set up, and nothing before the snapshot", () => {
    expect(selfHostedSummary({ ...fixtures.artifactStore, configured: false }, fixtures.config, now)).toBe("Not set up")
    expect(selfHostedSummary(undefined, fixtures.config, now)).toBeUndefined()
  })
})
