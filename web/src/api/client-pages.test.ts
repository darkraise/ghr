import { act, renderHook } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { mockApi, noContent } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { api } from "./client"
import {
  annotationsPoll,
  artifactStoreBusy,
  keys,
  logSettling,
  POLL_ACTIONS,
  POLL_SAFETY,
  POLL_SLOW,
  runDetailPoll,
  useJobLog,
  useRunAnnotations,
  useRunDetail,
} from "./hooks"

const ok = () => noContent()

describe("api for the later pages", () => {
  it.each([
    ["patchConfig", () => api.patchConfig({ global_max: 3, runner_limits: { cpu_quota: "200%" } }), "PATCH", "/api/config", { global_max: 3, runner_limits: { cpu_quota: "200%" } }],
    ["addRepo", () => api.addRepo({ name: "new-repo", allow_public: false }), "POST", "/api/repos", { name: "new-repo", allow_public: false }],
    ["removeRepo", () => api.removeRepo("dark mem"), "DELETE", "/api/repos/dark%20mem", undefined],
    ["pauseRepo", () => api.pauseRepo("darkmem"), "POST", "/api/repos/darkmem/pause", undefined],
    ["resumeRepo", () => api.resumeRepo("darkmem"), "POST", "/api/repos/darkmem/resume", undefined],
    ["startLabelCheck", () => api.startLabelCheck("darkmem"), "POST", "/api/repos/darkmem/label-check", undefined],
    ["deleteRegistration", () => api.deleteRegistration("darkmem", 7), "DELETE", "/api/repos/darkmem/registrations/7", undefined],
    ["queueRunnerUpdate", () => api.queueRunnerUpdate(), "POST", "/api/runner-update", undefined],
    ["cancelRunnerUpdate", () => api.cancelRunnerUpdate(), "DELETE", "/api/runner-update", undefined],
    ["runJob", () => api.runJob("prune"), "POST", "/api/jobs/prune/run", undefined],
    ["runJob escapes the name", () => api.runJob("a b/c"), "POST", "/api/jobs/a%20b%2Fc/run", undefined],
    ["installGhr", () => api.installGhr(), "POST", "/api/ghr-update", undefined],
    ["cancelGhrInstall", () => api.cancelGhrInstall(), "DELETE", "/api/ghr-update", undefined],
    ["refreshStorage", () => api.refreshStorage(), "POST", "/api/storage/refresh", undefined],
    ["installToolchain", () => api.installToolchain("node", "24"), "POST", "/api/toolchains", { tool: "node", version: "24" }],
    ["installPreset", () => api.installPreset("popular"), "POST", "/api/toolchains", { preset: "popular" }],
    ["removeToolchain", () => api.removeToolchain("node", "22.11.0"), "DELETE", "/api/toolchains/node/22.11.0", undefined],
    ["clearCache", () => api.clearCache("nuget"), "POST", "/api/caches/nuget/clear", undefined],
    ["prune standard", () => api.prune("standard"), "POST", "/api/prune", undefined],
    ["prune a scope", () => api.prune("unused-volumes"), "POST", "/api/prune/unused-volumes", undefined],
    ["refreshGitHubStorage", () => api.refreshGitHubStorage(), "POST", "/api/github-storage/refresh", undefined],
    ["deleteArtifact", () => api.deleteArtifact("dark mem", 9001), "DELETE", "/api/github-storage/dark%20mem/artifacts/9001", undefined],
    ["deleteCache", () => api.deleteCache("darkmem", 77), "DELETE", "/api/github-storage/darkmem/caches/77", undefined],
    ["deleteArtifacts by id", () => api.deleteArtifacts("darkmem", { ids: [9001, 9002] }), "POST", "/api/github-storage/darkmem/artifacts/delete", { ids: [9001, 9002] }],
    ["deleteArtifacts by age", () => api.deleteArtifacts("darkmem", { older_than: "2d" }), "POST", "/api/github-storage/darkmem/artifacts/delete", { older_than: "2d" }],
    ["deleteCaches by id", () => api.deleteCaches("darkmem", { ids: [77] }), "POST", "/api/github-storage/darkmem/caches/delete", { ids: [77] }],
    ["deleteCaches all", () => api.deleteCaches("darkmem", { all: true }), "POST", "/api/github-storage/darkmem/caches/delete", { all: true }],
    ["setRetention", () => api.setRetention("darkmem", 7), "PUT", "/api/github-storage/darkmem/retention", { days: 7 }],
    ["setRetentionAll", () => api.setRetentionAll(5), "POST", "/api/github-storage/retention", { days: 5 }],
    ["refreshArtifactStore", () => api.refreshArtifactStore(), "POST", "/api/artifact-store/refresh", undefined],
    ["testArtifactStore", () => api.testArtifactStore(), "POST", "/api/artifact-store/test", undefined],
    ["setArtifactKey", () => api.setArtifactKey("job", "JOBKEXAMPLE", "s3cr3t"), "PUT", "/api/artifact-store/keys", { which: "job", id: "JOBKEXAMPLE", secret: "s3cr3t" }],
    ["setArtifactRepo on", () => api.setArtifactRepo("dark mem", { enabled: true }), "PUT", "/api/artifact-store/repos/dark%20mem", { enabled: true }],
    ["setArtifactRepo cleanup", () => api.setArtifactRepo("darkmem", { cleanup: "3d" }), "PUT", "/api/artifact-store/repos/darkmem", { cleanup: "3d" }],
    ["enablePrivateArtifactRepos", () => api.enablePrivateArtifactRepos(), "POST", "/api/artifact-store/repos/enable-private", undefined],
    [
      "deleteArtifactObjects",
      () => api.deleteArtifactObjects("darkmem", ["darkraise/darkmem/95/frontend-dist.zip"]),
      "POST",
      "/api/artifact-store/darkmem/artifacts/delete",
      { keys: ["darkraise/darkmem/95/frontend-dist.zip"] },
    ],
    ["artifactLink", () => api.artifactLink("darkraise/darkmem/102/e2e-report.zip", "24h"), "POST", "/api/artifact-store/link", { key: "darkraise/darkmem/102/e2e-report.zip", ttl: "24h" }],
  ])("%s sends its request", async (_name, call, method, path, body) => {
    const { calls } = mockApi({ [`${method} ${path}`]: ok })
    await call()
    expect(calls).toHaveLength(1)
    expect(calls[0]).toMatchObject({ method, path, body, contentType: body === undefined ? undefined : "application/json" })
  })

  it("asks for activity by window and zone, and by repo when given", async () => {
    const { calls } = mockApi({ "GET /api/activity": { window: "3h" } })
    expect((await api.activity("3h", "Asia/Ho_Chi_Minh", "")).window).toBe("3h")
    await api.activity("24h", "UTC", "darkmem")
    expect(calls[0]?.search).toBe("?window=3h&tz=Asia%2FHo_Chi_Minh")
    expect(calls[1]?.search).toBe("?window=24h&tz=UTC&repo=darkmem")
  })

  it("reads the GitHub storage snapshot and a cleanup preview", async () => {
    const preview = [{ repo: "darkmem", count: 2, bytes: 209715200 }]
    const { calls } = mockApi({ "GET /api/github-storage": fixtures.githubStorage, "GET /api/github-storage/preview": preview })
    expect((await api.githubStorage()).repos).toHaveLength(3)
    expect(await api.storagePreview("2d")).toEqual(preview)
    await api.storagePreview("off", "dark mem")
    expect(calls.map((c) => c.search)).toEqual(["", "?cleanup=2d", "?cleanup=off&repo=dark+mem"])
  })

  it("reads the artifact store snapshot and a run's objects", async () => {
    const items = fixtures.artifactStore.repos[0]?.items.filter((i) => i.run_id === 102) ?? []
    const { calls } = mockApi({ "GET /api/artifact-store": fixtures.artifactStore, "GET /api/artifact-store/darkmem/runs/102": items })
    expect((await api.artifactStore()).repos).toHaveLength(4)
    expect((await api.artifactRunObjects("darkmem", 102)).map((i) => i.name)).toEqual(["e2e-report", "coverage-report"])
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual(["GET /api/artifact-store", "GET /api/artifact-store/darkmem/runs/102"])
  })

  it("sends a replacement token as plain text, unchanged", async () => {
    const { calls } = mockApi({ "PUT /api/token": ok })
    await api.replaceToken("github_pat_abc")
    expect(calls[0]).toMatchObject({ method: "PUT", path: "/api/token", body: "github_pat_abc", contentType: "text/plain" })
  })

  it("reads the reload warnings", async () => {
    mockApi({ "POST /api/reload": ["web settings changed; restart ghr to apply"] })
    expect(await api.reload()).toEqual(["web settings changed; restart ghr to apply"])
  })

  it("reads the per-repo and storage endpoints", async () => {
    const { calls } = mockApi({
      "GET /api/repos/available": [],
      "GET /api/repos/darkmem/label-check": { state: "not_checked", partial: false, groups: [] },
      "GET /api/repos/darkmem/registrations": [],
      "GET /api/token": { state: "ok" },
      "GET /api/storage": { measuring: false },
      "GET /api/toolchains/available": [],
    })
    await api.availableRepos()
    await api.labelCheck("darkmem")
    await api.registrations("darkmem")
    await api.token()
    await api.storage()
    await api.toolchainChoices("node")
    expect(calls.map((c) => `${c.path}${c.search}`)).toEqual([
      "/api/repos/available",
      "/api/repos/darkmem/label-check",
      "/api/repos/darkmem/registrations",
      "/api/token",
      "/api/storage",
      "/api/toolchains/available?tool=node",
    ])
  })
})

describe("api for the run pages", () => {
  it.each([
    ["runDetail latest", () => api.runDetail("dark mem", 102, undefined), "/api/actions/dark%20mem/runs/102", ""],
    ["runDetail attempt", () => api.runDetail("darkmem", 102, 1), "/api/actions/darkmem/runs/102", "?attempt=1"],
    ["runAnnotations latest", () => api.runAnnotations("darkmem", 102, undefined), "/api/actions/darkmem/runs/102/annotations", ""],
    ["runAnnotations attempt", () => api.runAnnotations("darkmem", 102, 2), "/api/actions/darkmem/runs/102/annotations", "?attempt=2"],
    ["jobLog", () => api.jobLog("darkmem", 1002), "/api/actions/darkmem/jobs/1002/log", ""],
    ["rawJobLog", () => api.rawJobLog("darkmem", 1002), "/api/actions/darkmem/jobs/1002/log/raw", ""],
    ["artifactDownload", () => api.artifactDownload("darkmem", 11), "/api/actions/darkmem/artifacts/11/download", ""],
  ])("%s reads its route", async (_name, call, path, search) => {
    const { calls } = mockApi({ [`GET ${path}`]: { ok: true } })
    await call()
    expect(calls).toHaveLength(1)
    expect(calls[0]).toMatchObject({ method: "GET", path, search })
  })

  it("keys a run by its lower-case repository, ID and attempt", () => {
    expect(keys.runDetail("DarkMem", 102, undefined)).toEqual(["run-detail", "darkmem", 102, 0])
    expect(keys.runAnnotations("darkmem", 102, 1)).toEqual(["run-annotations", "darkmem", 102, 1])
    expect(keys.jobLog("DarkMem", 1002)).toEqual(["job-log", "darkmem", 1002])
  })

  it("keys a run's objects under the artifact store, so a store change refetches them", () => {
    expect(keys.artifactStore).toEqual(["artifact-store"])
    expect(keys.artifactRunObjects("darkmem", 102)).toEqual(["artifact-store", "run", "darkmem", 102])
  })

  it.each([
    ["no data, latest attempt", undefined, undefined, false],
    ["no data, numbered attempt", undefined, 2, false],
    ["an active run, latest attempt", { ...fixtures.runDetail, status: "in_progress" }, undefined, POLL_ACTIONS],
    ["an active run, numbered attempt", { ...fixtures.runDetail, status: "in_progress" }, 2, POLL_ACTIONS],
    ["a completed run, latest attempt", fixtures.runDetail, undefined, POLL_SAFETY],
    ["a completed run, numbered attempt", fixtures.runDetail, 2, false],
  ])("polls a run's details with %s", (_why, data, attempt, want) => {
    expect(runDetailPoll(data, attempt)).toBe(want)
  })

  it.each([
    ["no data on an active run", undefined, true, POLL_ACTIONS],
    ["no data on a finished run", undefined, false, false],
    ["unread logs", { ...fixtures.runAnnotations, unread: 2 }, false, POLL_SLOW],
    ["every log read on an active run", fixtures.runAnnotations, true, POLL_ACTIONS],
    ["every log read on a finished run", fixtures.runAnnotations, false, false],
  ])("polls annotations with %s", (_why, data, active, want) => {
    expect(annotationsPoll(data, active)).toBe(want)
  })

  it("reads nothing while the hooks are disabled", async () => {
    const { calls } = mockApi({})
    const { wrapper } = withQuery()
    renderHook(() => useRunDetail("darkmem", 102, undefined, false), { wrapper })
    renderHook(() => useRunAnnotations("darkmem", 102, undefined, true, false), { wrapper })
    renderHook(() => useJobLog("darkmem", 1002, false, false), { wrapper })
    await new Promise((r) => setTimeout(r, 100))
    expect(calls).toHaveLength(0)
  })
})

describe("logSettling", () => {
  const completed = "2026-10-03T14:00:00Z"
  const at = Date.parse(completed)
  it.each([
    ["no completion time", undefined, at, false],
    ["just completed", completed, at, true],
    ["completed 30 seconds ago", completed, at + 30_000, true],
    ["completed exactly 3 minutes ago", completed, at + 180_000, false],
    ["completed 10 minutes ago", completed, at + 600_000, false],
    ["a clock behind the completion time", completed, at - 5_000, false],
  ])("is %s", (_why, completedAt, now, want) => {
    expect(logSettling(completedAt, now)).toBe(want)
  })
})

describe("useJobLog", () => {
  it("asks again only while it is told the log is settling", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] })
    try {
      const { calls } = mockApi({ "GET /api/actions/darkmem/jobs/1002/log": { ...fixtures.jobLog, no_log: true, steps: [] } })
      const { wrapper } = withQuery()
      const { rerender } = renderHook(({ settling }: { settling: boolean }) => useJobLog("darkmem", 1002, true, settling), {
        wrapper,
        initialProps: { settling: true },
      })
      await act(() => vi.advanceTimersByTimeAsync(15_000))
      expect(calls).toHaveLength(2)
      rerender({ settling: false })
      await act(() => vi.advanceTimersByTimeAsync(30_000))
      expect(calls).toHaveLength(2)
    } finally {
      vi.useRealTimers()
    }
  })
})

describe("artifactStoreBusy", () => {
  const op = { id: "op-9", kind: "delete-objects", repo: "darkmem", total: 2, done: 1, bytes: 0, failed: [], started_at: "2026-10-03T14:04:00Z" }
  it.each([
    ["no snapshot", undefined, false],
    ["no operation", fixtures.artifactStore, false],
    ["a running delete", { ...fixtures.artifactStore, operation: op }, true],
    ["a finished delete", { ...fixtures.artifactStore, operation: { ...op, finished_at: "2026-10-03T14:04:30Z" } }, false],
  ])("is %s", (_why, store, want) => {
    expect(artifactStoreBusy(store)).toBe(want)
  })
})
