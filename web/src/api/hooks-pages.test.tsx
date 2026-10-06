import { renderHook, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { LabelCheck, Status, Storage } from "@/api/types"
import { mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { storageBusy, useLabelCheck, useRepoActivity } from "./hooks"

const idle: Storage = {
  ...fixtures.storage,
  measuring: false,
  operations: { current: null, queued: 0, recent: [] },
}
const calm: Status = { ...fixtures.status, maintenance: { running: false } }

describe("storageBusy", () => {
  it("is false when nothing runs", () => {
    expect(storageBusy(idle, calm)).toBe(false)
    expect(storageBusy(undefined, undefined)).toBe(false)
  })

  it.each([
    ["an operation runs", { ...idle, operations: { ...idle.operations, current: fixtures.storage.operations.current } }, calm],
    ["an operation is queued", { ...idle, operations: { ...idle.operations, queued: 2 } }, calm],
    ["a measurement runs", { ...idle, measuring: true }, calm],
    ["a prune runs", idle, { ...calm, maintenance: { running: true } }],
  ])("is true when %s", (_why, storage, status) => {
    expect(storageBusy(storage, status)).toBe(true)
  })
})

describe("useLabelCheck", () => {
  it("polls while the check runs and stops when it is done", { timeout: 10_000 }, async () => {
    let reads = 0
    const { calls } = mockApi({
      "GET /api/repos/darkmem/label-check": (): LabelCheck => {
        reads++
        return reads < 3 ? { state: "checking", partial: false, groups: [] } : fixtures.labelCheck
      },
    })
    const { wrapper } = withQuery()
    const { result } = renderHook(() => useLabelCheck("darkmem", true), { wrapper })
    await waitFor(() => expect(result.current.data?.state).toBe("done"), { timeout: 6000 })
    const settled = calls.length
    await new Promise((r) => setTimeout(r, 1500))
    expect(calls.length).toBe(settled)
  })

  it("does not read while disabled", async () => {
    const { calls } = mockApi({ "GET /api/repos/darkmem/label-check": fixtures.labelCheck })
    const { wrapper } = withQuery()
    renderHook(() => useLabelCheck("darkmem", false), { wrapper })
    await new Promise((r) => setTimeout(r, 300))
    expect(calls).toHaveLength(0)
  })
})

describe("useRepoActivity", () => {
  it("reads up to 500 history entries for the repo", async () => {
    const { calls } = mockApi({ "GET /api/history": fixtures.history })
    const { wrapper } = withQuery()
    const { result } = renderHook(() => useRepoActivity("darkmem"), { wrapper })
    await waitFor(() => expect(result.current.data).toHaveLength(2))
    expect(calls[0]?.search).toBe("?repo=darkmem&conclusion=&limit=500")
  })
})
