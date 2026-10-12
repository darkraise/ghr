import { act, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"
import { keys } from "@/api/hooks"
import { json, mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { RepoPicker } from "./repo-picker"

describe("RepoPicker", () => {
  it("keeps the list when a read fails, with a way to retry", async () => {
    let down = false
    mockApi({
      "GET /api/repos/available": () => (down ? json({ error: "GitHub is not answering" }, 502) : fixtures.availableRepos),
    })
    const { queryClient, wrapper } = withQuery()
    const user = userEvent.setup()
    render(<RepoPicker picked="" onPick={() => {}} />, { wrapper })
    expect(await screen.findByText("new-repo")).toBeInTheDocument()
    down = true
    await act(() => queryClient.refetchQueries({ queryKey: keys.availableRepos }))
    expect(await screen.findByText("GitHub is not answering")).toBeInTheDocument()
    expect(screen.getByText("new-repo")).toBeInTheDocument()
    down = false
    await user.click(screen.getByRole("button", { name: "Try again" }))
    await waitFor(() => expect(screen.queryByText("GitHub is not answering")).toBeNull())
    expect(screen.getByText("new-repo")).toBeInTheDocument()
  })

  it("shows only the error while there is no list yet", async () => {
    mockApi({ "GET /api/repos/available": () => json({ error: "GitHub is not answering" }, 502) })
    const { wrapper } = withQuery()
    render(<RepoPicker picked="" onPick={() => {}} />, { wrapper })
    expect(await screen.findByText("GitHub is not answering")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument()
    expect(screen.queryByLabelText("Filter repositories")).toBeNull()
  })
})
