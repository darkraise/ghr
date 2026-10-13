import { render, screen, within } from "@testing-library/react"
import type { ReactNode } from "react"
import { describe, expect, it } from "vitest"
import { keys } from "@/api/hooks"
import type { Deletion, GitHubOperation } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { OperationProgress, RecentDeletions } from "./storage-operations"

const finished = fixtures.githubStorage.operation as GitHubOperation

function draw(ui: ReactNode) {
  const { queryClient, wrapper } = withQuery()
  // useNow reads the daemon's clock from the status, here 2026-10-03.
  queryClient.setQueryData(keys.status, fixtures.status)
  return render(ui, { wrapper })
}

describe("OperationProgress", () => {
  it("shows a running operation's progress", () => {
    const op: GitHubOperation = { ...finished, kind: "delete-caches", done: 1, total: 4, bytes: 71431401, failed: [], finished_at: undefined }
    draw(<OperationProgress operation={op} />)
    const region = screen.getByRole("region", { name: "Operation" })
    expect(within(region).getByText("Deleting caches in darkmem")).toBeInTheDocument()
    expect(within(region).getByText("1 of 4, 71.4 MB freed")).toBeInTheDocument()
    const bar = within(region).getByRole("progressbar", { name: "Deleting caches in darkmem" })
    expect(bar).toHaveAttribute("aria-valuenow", "1")
    expect(bar).toHaveAttribute("aria-valuemax", "4")
  })

  it("shows a finished operation with its failures", () => {
    draw(<OperationProgress operation={finished} />)
    const region = screen.getByRole("region", { name: "Operation" })
    expect(within(region).getByText("Deleted artifacts in darkmem")).toBeInTheDocument()
    expect(within(region).getByText("3 of 3, 52.4 MB freed, 1 failed, finished 13:56")).toBeInTheDocument()
    expect(within(region).getByText("darkmem 9003: not in the last inventory")).toBeInTheDocument()
    expect(within(region).queryByRole("progressbar")).toBeNull()
  })

  it("names a retention change across repositories", () => {
    const op: GitHubOperation = {
      ...finished,
      kind: "set-retention",
      repo: undefined,
      bytes: 0,
      failed: [{ repo: "darkcloud", error: "its maximum is 90 days" }],
    }
    draw(<OperationProgress operation={op} />)
    expect(screen.getByText("Set retention on every repository")).toBeInTheDocument()
    expect(screen.getByText("3 of 3, 1 failed, finished 13:56")).toBeInTheDocument()
    expect(screen.getByText("darkcloud: its maximum is 90 days")).toBeInTheDocument()
  })

  it("shows nothing before any operation", () => {
    const { container } = draw(<OperationProgress operation={undefined} />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe("RecentDeletions", () => {
  it("lists what was deleted, newest first, and by whom", () => {
    draw(<RecentDeletions deletions={fixtures.githubStorage.deletions} />)
    const region = screen.getByRole("region", { name: "Recent deletions" })
    const rows = within(region).getAllByRole("row").slice(1)
    expect(rows.map((r) => [...r.querySelectorAll("td, th")].map((c) => c.textContent))).toEqual([
      ["13:56", "darkmem", "artifact", "coverage-old", "52.4 MB", "3d", "Cleanup"],
      ["12:05", "darkcloud", "cache", "node-cache-Linux-x64-npm-0b1c", "31.5 MB", "5d", "Manual"],
    ])
  })

  it("says when nothing was deleted", () => {
    const none: Deletion[] = []
    draw(<RecentDeletions deletions={none} />)
    expect(within(screen.getByRole("region", { name: "Recent deletions" })).getByText("Nothing deleted since ghr started.")).toBeInTheDocument()
  })
})
