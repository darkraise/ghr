import { describe, expect, it } from "vitest"
import { api } from "@/api/client"
import type { SetupState } from "@/api/types"
import { mockApi, noContent } from "@/test/api"

describe("setup client", () => {
  it("reads the setup state and posts GitHub and finish", async () => {
    const state: SetupState = {
      configured: false,
      starting: false,
      setup_pending: true,
      toolchains_pending: true,
      owner: "",
      web_listen: "0.0.0.0:8080",
    }
    const { calls } = mockApi({
      "GET /api/setup": state,
      "POST /api/setup/github": () => noContent(),
      "POST /api/setup/finish": () => noContent(),
    })
    expect(await api.setupState()).toEqual(state)
    await api.setupGitHub("DarkRaise", "tok")
    await api.setupFinish("popular")
    expect(calls.map((c) => [c.method, c.path, c.body])).toEqual([
      ["GET", "/api/setup", undefined],
      ["POST", "/api/setup/github", { owner: "DarkRaise", token: "tok" }],
      ["POST", "/api/setup/finish", { toolchains: "popular" }],
    ])
  })
})
