import { act, renderHook, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { RepoStatus } from "@/api/types"
import { mockApi, noContent } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { useRepoActions } from "./use-repo-actions"

const darkmem = fixtures.status.repos[0] as RepoStatus
const darkcloud = fixtures.status.repos[1] as RepoStatus

function setup(repo: RepoStatus, offline = false) {
  const { wrapper } = withQuery()
  return renderHook(() => useRepoActions(repo, offline), { wrapper })
}

describe("useRepoActions", () => {
  it("pauses a running repository and resumes a paused one", async () => {
    const { calls } = mockApi({
      "POST /api/repos/darkmem/pause": () => noContent(),
      "POST /api/repos/darkcloud/resume": () => noContent(),
    })
    const running = setup(darkmem)
    const paused = setup(darkcloud)
    act(() => running.result.current.togglePause())
    act(() => paused.result.current.togglePause())
    await waitFor(() => expect(calls.map((c) => c.path).sort()).toEqual(["/api/repos/darkcloud/resume", "/api/repos/darkmem/pause"]))
  })

  it("asks before removing, then removes", async () => {
    const { calls } = mockApi({ "DELETE /api/repos/darkmem": () => noContent() })
    const { result } = setup(darkmem)
    act(() => result.current.askRemove())
    expect(result.current.confirm?.title).toBe("Remove repo darkmem? Its running jobs finish first.")
    expect(calls).toHaveLength(0)
    act(() => result.current.confirm?.run())
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/repos/darkmem")).toBe(true))
    act(() => result.current.closeConfirm())
    expect(result.current.confirm).toBeNull()
  })

  it("locks a repository being removed, or while the daemon is unreachable", () => {
    expect(setup({ ...darkmem, removing: true }).result.current.locked).toBe(true)
    expect(setup(darkmem, true).result.current.locked).toBe(true)
    expect(setup(darkmem).result.current.locked).toBe(false)
  })
})
