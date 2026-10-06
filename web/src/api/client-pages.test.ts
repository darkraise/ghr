import { describe, expect, it } from "vitest"
import { mockApi, noContent } from "@/test/api"
import { api } from "./client"

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
    ["refreshStorage", () => api.refreshStorage(), "POST", "/api/storage/refresh", undefined],
    ["installToolchain", () => api.installToolchain("node", "24"), "POST", "/api/toolchains", { tool: "node", version: "24" }],
    ["installPreset", () => api.installPreset("popular"), "POST", "/api/toolchains", { preset: "popular" }],
    ["removeToolchain", () => api.removeToolchain("node", "22.11.0"), "DELETE", "/api/toolchains/node/22.11.0", undefined],
    ["clearCache", () => api.clearCache("nuget"), "POST", "/api/caches/nuget/clear", undefined],
    ["prune standard", () => api.prune("standard"), "POST", "/api/prune", undefined],
    ["prune a scope", () => api.prune("unused-volumes"), "POST", "/api/prune/unused-volumes", undefined],
  ])("%s sends its request", async (_name, call, method, path, body) => {
    const { calls } = mockApi({ [`${method} ${path}`]: ok })
    await call()
    expect(calls).toHaveLength(1)
    expect(calls[0]).toMatchObject({ method, path, body })
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
