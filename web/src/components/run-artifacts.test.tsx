import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import { json, mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { ArtifactsSection } from "./run-artifacts"

const row = (name: string) => within(screen.getByText(name).closest("tr") as HTMLElement)
const objects = fixtures.artifactStore.repos[0]?.items.filter((i) => i.run_id === 102) ?? []
const OBJECTS = "GET /api/artifact-store/darkmem/runs/102"
const url = "https://s3.example.sg/gha-artifacts/darkraise/darkmem/102/e2e-report.zip?X-Amz-Signature=c3"
const cells = () =>
  screen
    .getAllByRole("row")
    .slice(1)
    .map((r) => [...r.querySelectorAll("td")].slice(0, 2).map((c) => c.textContent))

afterEach(() => {
  Reflect.deleteProperty(navigator, "clipboard")
  Reflect.deleteProperty(window, "isSecureContext")
})

describe("ArtifactsSection", () => {
  it("lists each artifact's size and expiry", () => {
    const { wrapper } = withQuery()
    render(<ArtifactsSection repo="darkmem" runId={102} artifacts={fixtures.runDetail.artifacts} selfHosted={false} active={false} />, { wrapper })
    expect(screen.getByRole("region", { name: "Artifacts" })).toBeInTheDocument()
    expect(screen.getByText("frontend-dist")).toHaveClass("font-mono")
    expect(row("frontend-dist").getByText("2.4 MB")).toHaveClass("font-mono", "text-right")
    expect(row("frontend-dist").getByText("2026-10-17")).toHaveClass("font-mono", "text-right")
    expect(row("frontend-dist").getByText("GitHub")).toBeInTheDocument()
    expect(row("frontend-dist").getByRole("button", { name: "Download frontend-dist" })).toBeInTheDocument()
  })

  it("shows an expired artifact without a download button", () => {
    const { wrapper } = withQuery()
    render(<ArtifactsSection repo="darkmem" runId={102} artifacts={fixtures.runDetail.artifacts} selfHosted={false} active={false} />, { wrapper })
    expect(row("coverage-backend").getByText("Expired")).toHaveClass("text-muted-foreground")
    expect(row("coverage-backend").queryByRole("button")).toBeNull()
  })

  it("asks the daemon for the address, then goes to it", async () => {
    const github = "https://productionresultssa1.blob.core.windows.net/actions-results/a.zip?sig=x"
    const { calls } = mockApi({ "GET /api/actions/darkmem/artifacts/11/download": { url: github } })
    const assign = vi.fn()
    vi.stubGlobal("location", { assign })
    const { wrapper } = withQuery()
    render(<ArtifactsSection repo="darkmem" runId={102} artifacts={fixtures.runDetail.artifacts} selfHosted={false} active={false} />, { wrapper })
    await userEvent.setup().click(screen.getByRole("button", { name: "Download frontend-dist" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith(github))
    expect(calls.map((c) => c.path)).toEqual(["/api/actions/darkmem/artifacts/11/download"])
  })

  it("renders nothing for a run without artifacts", () => {
    const { wrapper } = withQuery()
    const { container } = render(<ArtifactsSection repo="darkmem" runId={102} artifacts={[]} selfHosted={false} active={false} />, { wrapper })
    expect(container).toBeEmptyDOMElement()
  })

  it("lists the run's RustFS objects beside its GitHub artifacts, each with its store", async () => {
    mockApi({ [OBJECTS]: objects })
    const { wrapper } = withQuery()
    render(<ArtifactsSection repo="darkmem" runId={102} artifacts={fixtures.runDetail.artifacts} selfHosted active={false} />, { wrapper })
    expect(await screen.findByText("e2e-report")).toHaveClass("font-mono")
    expect(cells()).toEqual([
      ["e2e-report", "RustFS"],
      ["coverage-report", "RustFS"],
      ["frontend-dist", "GitHub"],
      ["coverage-backend", "GitHub"],
    ])
    expect(row("e2e-report").getByText("8.9 MB")).toBeInTheDocument()
    expect(row("e2e-report").getByText("2026-10-08")).toBeInTheDocument()
    expect(row("e2e-report").getByRole("button", { name: "Download e2e-report" })).toBeInTheDocument()
    expect(row("e2e-report").getByRole("button", { name: "Copy a link to e2e-report" })).toBeInTheDocument()
    expect(row("frontend-dist").queryByRole("button", { name: /^Copy a link/ })).toBeNull()
    expect(screen.getByText("4 artifacts · 2 on RustFS")).toBeInTheDocument()
    expect(
      screen.getByText("GitHub artifacts here come from steps that still use actions/upload-artifact, or from before the repository was turned on."),
    ).toBeInTheDocument()
  })

  it("downloads a RustFS object through a fresh one-hour link", async () => {
    const { calls } = mockApi({ [OBJECTS]: objects, "POST /api/artifact-store/link": { url, expires_at: "2026-10-03T15:05:00Z" } })
    const assign = vi.fn()
    vi.stubGlobal("location", { assign })
    const { wrapper } = withQuery()
    render(<ArtifactsSection repo="darkmem" runId={102} artifacts={fixtures.runDetail.artifacts} selfHosted active={false} />, { wrapper })
    await userEvent.setup().click(await screen.findByRole("button", { name: "Download e2e-report" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith(url))
    expect(calls.filter((c) => c.method === "POST").map((c) => c.body)).toEqual([{ key: "darkraise/darkmem/102/e2e-report.zip", ttl: "1h" }])
  })

  it("copies a link to a RustFS object for the chosen time", async () => {
    const { calls } = mockApi({ [OBJECTS]: objects, "POST /api/artifact-store/link": { url, expires_at: "2026-10-10T14:05:00Z" } })
    const { wrapper } = withQuery()
    render(<ArtifactsSection repo="darkmem" runId={102} artifacts={fixtures.runDetail.artifacts} selfHosted active={false} />, { wrapper })
    const user = userEvent.setup()
    Object.defineProperty(window, "isSecureContext", { value: true, configurable: true })
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    await user.click(await screen.findByRole("button", { name: "Copy a link to e2e-report" }))
    await user.click(await screen.findByRole("menuitem", { name: "7 days" }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(url))
    expect(calls.filter((c) => c.method === "POST").map((c) => c.body)).toEqual([{ key: "darkraise/darkmem/102/e2e-report.zip", ttl: "7d" }])
  })

  it("asks for no RustFS objects while the store is not set up", async () => {
    const { calls } = mockApi({})
    const { wrapper } = withQuery()
    render(<ArtifactsSection repo="darkmem" runId={102} artifacts={fixtures.runDetail.artifacts} selfHosted={false} active={false} />, { wrapper })
    await new Promise((r) => setTimeout(r, 100))
    expect(calls).toHaveLength(0)
  })

  it("shows a run whose only artifacts are on RustFS", async () => {
    mockApi({ [OBJECTS]: objects })
    const { wrapper } = withQuery()
    render(<ArtifactsSection repo="darkmem" runId={102} artifacts={[]} selfHosted active={false} />, { wrapper })
    expect(await screen.findByText("e2e-report")).toBeInTheDocument()
    expect(screen.getByText("2 artifacts · 2 on RustFS")).toBeInTheDocument()
    expect(screen.queryByText(/^GitHub artifacts here come from/)).toBeNull()
  })

  it("says when the RustFS listing failed and keeps GitHub's rows", async () => {
    mockApi({ [OBJECTS]: () => json({ error: "no admin key set" }, 500) })
    const { wrapper } = withQuery()
    render(<ArtifactsSection repo="darkmem" runId={102} artifacts={fixtures.runDetail.artifacts} selfHosted active={false} />, { wrapper })
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not list the RustFS artifacts: no admin key set")
    expect(row("frontend-dist").getByRole("button", { name: "Download frontend-dist" })).toBeInTheDocument()
  })
})
