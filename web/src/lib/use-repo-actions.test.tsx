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
    expect(result.current.confirm).toMatchObject({ title: "Remove darkmem?", body: "Its running jobs finish first.", action: "Remove" })
    expect(calls).toHaveLength(0)
    act(() => result.current.confirm?.run())
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/repos/darkmem")).toBe(true))
    act(() => result.current.closeConfirm())
    expect(result.current.confirm).toBeNull()
  })

  it.each([
    ["a repository being removed", { ...darkmem, removing: true }, false, true, false],
    ["an unreachable daemon", darkmem, true, true, true],
    ["a live repository", darkmem, false, false, false],
  ])("locks Remove and the toggle as %s needs", (_what, repo, offline, locked, toggleLocked) => {
    const { result } = setup(repo, offline)
    expect(result.current.locked).toBe(locked)
    expect(result.current.toggleLocked).toBe(toggleLocked)
  })

  it.each([
    [darkmem, "Pause"],
    [darkcloud, "Resume"],
    [{ ...darkmem, removing: true }, "Cancel removal"],
    [{ ...darkcloud, removing: true }, "Cancel removal"],
  ])("names the toggle after what it does (%#)", (repo, label) => {
    expect(setup(repo).result.current.toggleLabel).toBe(label)
  })

  it("cancels a removal by resuming the repository", async () => {
    const { calls } = mockApi({ "POST /api/repos/darkmem/resume": () => noContent() })
    const { result } = setup({ ...darkmem, removing: true })
    act(() => result.current.togglePause())
    await waitFor(() => expect(calls.map((c) => c.path)).toEqual(["/api/repos/darkmem/resume"]))
  })
})
