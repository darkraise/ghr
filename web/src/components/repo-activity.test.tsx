import { act, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { RepoStatus } from "@/api/types"
import { json, mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { setViewport } from "@/test/media"
import { withQuery } from "@/test/query"
import { RepoActivity } from "./repo-activity"

const now = Date.parse("2026-10-03T14:05:00Z")
const darkmem = fixtures.status.repos[0] as RepoStatus

function draw(withStatus = true) {
  const { wrapper } = withQuery()
  return render(<RepoActivity name="darkmem" repo={withStatus ? darkmem : undefined} now={now} />, { wrapper })
}

describe("RepoActivity", () => {
  it("asks for the repository's last 24 hours and draws only its jobs, without picking", async () => {
    const { calls } = mockApi({ "GET /api/activity": fixtures.activityBuckets })
    draw()
    expect(await screen.findByRole("group", { name: "Jobs per bucket for the last 24 hours" })).toBeInTheDocument()
    expect(screen.getByRole("region", { name: "Last 24 hours" })).toBeInTheDocument()
    expect(calls[0]?.search).toBe("?window=24h&tz=UTC&repo=darkmem")
    expect(screen.queryAllByRole("button")).toHaveLength(0)
  })

  it("lists the runners, waiting jobs, the week and the last job", async () => {
    mockApi({ "GET /api/activity": fixtures.activityBuckets })
    draw()
    expect(await screen.findByText("1 succeeded, 1 failed, 50%")).toBeInTheDocument()
    expect(screen.getByText("1 of 2")).toBeInTheDocument()
    const runners = screen.getByText("Runners").closest("div") as HTMLElement
    expect(runners.querySelectorAll('[data-slot="runner"]')).toHaveLength(2)
    expect((screen.getByText("Waiting").closest("div") as HTMLElement).textContent).toBe("Waiting3")
    expect(screen.getByRole("img", { name: "Succeeded" })).toBeInTheDocument()
    expect(screen.getByText("25m ago")).toBeInTheDocument()
  })

  it("puts the facts beside the chart at 1280px and under it below", async () => {
    mockApi({ "GET /api/activity": fixtures.activityBuckets })
    const media = setViewport(1280)
    draw()
    expect(screen.getByText("Runners").closest("dl")).toHaveClass("w-56")
    act(() => media.resize(1024))
    expect(screen.getByText("Runners").closest("dl")).toHaveClass("grid-cols-2")
  })

  it("leaves the facts blank without a status, and says when no job has finished", () => {
    mockApi({ "GET /api/activity": () => new Promise(() => {}) })
    draw(false)
    expect(screen.queryByText("1 of 2")).toBeNull()
    expect(screen.getByText("None yet")).toBeInTheDocument()
    expect(screen.getByText("Loading")).toBeInTheDocument()
  })

  it("offers a retry when the activity cannot be read", async () => {
    mockApi({ "GET /api/activity": () => json({ error: "history file unreadable" }, 500) })
    draw()
    expect(await screen.findByRole("alert")).toHaveTextContent("history file unreadable")
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument()
  })
})
