import { act, renderHook, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Config, ConfigPatch } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { GENERAL_KEYS, JOB_KEYS, useSettingsForm } from "./settings-form"

function daemon(patch: unknown = () => noContent(), config: Config = fixtures.config) {
  return mockApi({ "GET /api/config": config, "GET /api/status": fixtures.status, "PATCH /api/config": patch })
}

async function form(fields?: readonly string[]) {
  const hook = renderHook(() => useSettingsForm(fields), { wrapper: withQuery().wrapper })
  await waitFor(() => expect(hook.result.current.config).toBeDefined())
  return hook.result
}

describe("useSettingsForm with the jobs keys", () => {
  it("loads the jobs block as hours and minutes", async () => {
    daemon()
    const f = await form(JOB_KEYS)
    expect(f.current.values["jobs.prune.every"]).toBe(24)
    expect(f.current.values["jobs.storage_measure.every"]).toBe(30)
    expect(f.current.values["jobs.ghr_update_check.enabled"]).toBe(true)
    expect(JOB_KEYS).toEqual([
      "jobs.ghr_update_check.enabled",
      "jobs.ghr_update_check.every",
      "jobs.runner_update_check.enabled",
      "jobs.runner_update_check.every",
      "jobs.prune.enabled",
      "jobs.prune.every",
      "jobs.storage_measure.enabled",
      "jobs.storage_measure.every",
    ])
  })

  it("leaves the jobs keys out when an older daemon sends no jobs block", async () => {
    daemon(undefined, { ...fixtures.config, jobs: undefined })
    const f = await form(JOB_KEYS)
    expect(Object.keys(f.current.values).some((k) => k.startsWith("jobs."))).toBe(false)
  })

  it("counts and saves only its own keys", async () => {
    const { calls } = daemon()
    const f = await form(JOB_KEYS)
    act(() => {
      f.current.set("jobs.prune.every")(12)
      f.current.set("jobs.storage_measure.enabled")(false)
      f.current.set("global_max")(5)
    })
    expect([...f.current.changed].sort()).toEqual(["jobs.prune.every", "jobs.storage_measure.enabled"])
    await act(async () => {
      await f.current.save()
    })
    const patch = calls.find((c) => c.method === "PATCH")?.body as ConfigPatch
    expect(patch).toEqual({ jobs: { prune: { every: "12h" }, storage_measure: { enabled: false } } })
  })

  it("leaves the jobs keys out of the general form", async () => {
    daemon()
    const f = await form()
    act(() => f.current.set("jobs.prune.every")(12))
    expect(f.current.changed).toEqual([])
    expect(GENERAL_KEYS).not.toContain("jobs.prune.every")
    expect(GENERAL_KEYS).toContain("history_retention")
  })

  it("checks the minimums and places a refused interval on its field", async () => {
    daemon(() => json({ error: 'jobs.prune.every: invalid duration "2h"' }, 400))
    const f = await form(JOB_KEYS)
    act(() => {
      f.current.set("jobs.storage_measure.every")(4)
      f.current.set("global_max")(0)
    })
    expect(f.current.errors).toEqual({ "jobs.storage_measure.every": "Enter at least 5 minutes" })
    act(() => {
      f.current.set("jobs.storage_measure.every")(5)
      f.current.set("jobs.prune.every")(2)
    })
    expect(f.current.errors).toEqual({})
    await act(async () => {
      await f.current.save()
    })
    expect(f.current.rejectedFields).toEqual({ "jobs.prune.every": 'invalid duration "2h"' })
    expect(f.current.rejection).toBe("")
  })
})
