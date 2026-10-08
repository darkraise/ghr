import { describe, expect, it } from "vitest"
import type { Storage } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { lastPruneText, pruneRows, standardPrune, storageSummary } from "./storage"

describe("storageSummary", () => {
  it("names the use, the data root and the high-water mark", () => {
    expect(storageSummary(fixtures.status, 80)).toBe("Docker uses 61% of /var/lib/docker and prunes above 80%")
  })
  it("leaves out the root until it is known", () => {
    expect(storageSummary({ ...fixtures.status, disk_root: undefined }, 85)).toBe("Docker uses 61% and prunes above 85%")
  })
  it("says when nothing is measured yet", () => {
    expect(storageSummary({ ...fixtures.status, disk_total_bytes: 0 }, 80)).toBe("Not measured yet")
  })
})

describe("pruneRows", () => {
  it("says what each targeted prune removes and how much Docker can free", () => {
    expect(pruneRows(fixtures.storage, fixtures.config).map((r) => [r.scope, r.text, r.action, r.destructive])).toEqual([
      ["build-cache-keep", "Build cache beyond 20GB", "Trim build cache", false],
      ["build-cache-all", "All build cache, up to 380.0 MB", "Remove build cache", true],
      ["dangling-images", "Dangling images", "Remove dangling images", false],
      ["unused-volumes", "Unused volumes", "Remove unused volumes", true],
    ])
  })

  it("splits each confirmation into a question and its consequence", () => {
    const withVolumes: Storage = {
      ...fixtures.storage,
      docker: { ...fixtures.storage.docker, rows: [...(fixtures.storage.docker.rows ?? []), { type: "Local Volumes", count: 3, active: 1, bytes: 1_500_000_000, reclaimable: 1_200_000_000 }] },
    }
    expect(pruneRows(withVolumes, fixtures.config).map((r) => [r.text, r.title, r.body])).toEqual([
      ["Build cache beyond 20GB", "Trim the build cache to 20GB?", "Build cache beyond that size is removed."],
      ["All build cache, up to 380.0 MB", "Remove all build cache?", "Frees up to 380.0 MB. The next builds start cold."],
      ["Dangling images", "Remove dangling images?", "Images that are untagged and used by no container are removed."],
      [
        "Unused volumes, up to 1.2 GB",
        "Remove unused volumes?",
        "Frees up to 1.2 GB. Every volume no container uses is removed. It is refused while jobs run.",
      ],
    ])
  })

  it("names the keep size even before the config is read", () => {
    expect(pruneRows(fixtures.storage, undefined)[0]?.text).toBe("Build cache beyond the build cache keep size")
  })
})

describe("standardPrune", () => {
  it("asks before the standard prune", () => {
    expect(standardPrune(fixtures.config)).toEqual({
      title: "Prune now?",
      body: "Removes build cache beyond 20GB, dangling images, and history and logs past retention.",
      action: "Prune",
      destructive: false,
    })
  })
})

describe("lastPruneText", () => {
  it("names the trigger, scope, time and outcome", () => {
    const p = fixtures.storage.last_prune
    if (!p) throw new Error("fixture has a last prune")
    expect(lastPruneText(p)).toBe("Last prune: manual build-cache-all at 13:16, ok")
    expect(lastPruneText({ ...p, trigger: "auto" })).toBe("Last prune: auto at 13:16, ok")
  })
})
