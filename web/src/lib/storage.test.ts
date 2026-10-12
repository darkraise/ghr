import { describe, expect, it } from "vitest"
import type { Storage } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { lastPruneText, pruneActions, standardPrune, storageSummary } from "./storage"

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

describe("pruneActions", () => {
  it("ties each targeted prune to the Docker type it frees", () => {
    expect(pruneActions(fixtures.storage, fixtures.config).map((a) => [a.scope, a.type, a.label, a.action, a.destructive])).toEqual([
      ["build-cache-keep", "Build Cache", "Trim to 20GB", "Trim to 20GB", false],
      ["build-cache-all", "Build Cache", "Remove all", "Remove all build cache", true],
      ["dangling-images", "Images", "Remove dangling", "Remove dangling images", false],
      ["unused-volumes", "Local Volumes", "Remove unused", "Remove unused volumes", true],
    ])
  })

  it("starts each accessible name with the words on the button", () => {
    for (const a of pruneActions(fixtures.storage, fixtures.config)) expect(a.action.startsWith(a.label)).toBe(true)
  })

  it("splits each confirmation into a question and its consequence", () => {
    const withVolumes: Storage = {
      ...fixtures.storage,
      docker: { ...fixtures.storage.docker, rows: [...(fixtures.storage.docker.rows ?? []), { type: "Local Volumes", count: 3, active: 1, bytes: 1_500_000_000, reclaimable: 1_200_000_000 }] },
    }
    expect(pruneActions(withVolumes, fixtures.config).map((a) => [a.title, a.body])).toEqual([
      ["Trim the build cache to 20GB?", "Build cache beyond that size is removed."],
      ["Remove all build cache?", "Frees up to 380.0 MB. The next builds start cold."],
      ["Remove dangling images?", "Images that are untagged and used by no container are removed."],
      ["Remove unused volumes?", "Frees up to 1.2 GB. Every volume no container uses is removed. It is refused while jobs run."],
    ])
  })

  it("names the keep size even before the config is read", () => {
    expect(pruneActions(fixtures.storage, undefined)[0]?.label).toBe("Trim to the build cache keep size")
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
  const now = Date.parse("2026-10-03T14:05:00Z")
  const p = fixtures.storage.last_prune
  if (!p) throw new Error("fixture has a last prune")

  it("names the trigger, scope, time, outcome and what it freed", () => {
    expect(lastPruneText(p, now)).toBe("Last prune: manual build-cache-all at 13:16, ok, 6.8 MB freed")
    expect(lastPruneText({ ...p, trigger: "auto" }, now)).toBe("Last prune: auto at 13:16, ok, 6.8 MB freed")
  })

  it("leaves out what it freed when nothing was", () => {
    expect(lastPruneText({ ...p, steps: null }, now)).toBe("Last prune: manual build-cache-all at 13:16, ok")
  })

  it("names the day of a prune before today", () => {
    expect(lastPruneText({ ...p, finished_at: "2026-10-01T09:00:00Z" }, now)).toBe("Last prune: manual build-cache-all at Oct 1 09:00, ok, 6.8 MB freed")
  })
})
