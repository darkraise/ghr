import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { ArtifactsSection } from "./run-artifacts"

const row = (name: string) => within(screen.getByText(name).closest("tr") as HTMLElement)

describe("ArtifactsSection", () => {
  it("lists each artifact's size and expiry", () => {
    const { wrapper } = withQuery()
    render(<ArtifactsSection repo="darkmem" artifacts={fixtures.runDetail.artifacts} />, { wrapper })
    expect(screen.getByRole("region", { name: "Artifacts" })).toBeInTheDocument()
    expect(screen.getByText("frontend-dist")).toHaveClass("font-mono")
    expect(row("frontend-dist").getByText("2.4 MB")).toHaveClass("font-mono", "text-right")
    expect(row("frontend-dist").getByText("2026-10-17")).toHaveClass("font-mono", "text-right")
    expect(row("frontend-dist").getByRole("button", { name: "Download frontend-dist" })).toBeInTheDocument()
  })

  it("shows an expired artifact without a download button", () => {
    const { wrapper } = withQuery()
    render(<ArtifactsSection repo="darkmem" artifacts={fixtures.runDetail.artifacts} />, { wrapper })
    expect(row("coverage-backend").getByText("Expired")).toHaveClass("text-muted-foreground")
    expect(row("coverage-backend").queryByRole("button")).toBeNull()
  })

  it("asks the daemon for the address, then goes to it", async () => {
    const url = "https://productionresultssa1.blob.core.windows.net/actions-results/a.zip?sig=x"
    const { calls } = mockApi({ "GET /api/actions/darkmem/artifacts/11/download": { url } })
    const assign = vi.fn()
    vi.stubGlobal("location", { assign })
    const { wrapper } = withQuery()
    render(<ArtifactsSection repo="darkmem" artifacts={fixtures.runDetail.artifacts} />, { wrapper })
    await userEvent.setup().click(screen.getByRole("button", { name: "Download frontend-dist" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith(url))
    expect(calls.map((c) => c.path)).toEqual(["/api/actions/darkmem/artifacts/11/download"])
  })

  it("renders nothing for a run without artifacts", () => {
    const { wrapper } = withQuery()
    const { container } = render(<ArtifactsSection repo="darkmem" artifacts={[]} />, { wrapper })
    expect(container).toBeEmptyDOMElement()
  })
})
