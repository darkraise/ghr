import { screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"
import { SETUP_GUIDE } from "./artifact-store-setup"

async function draw(routes: Record<string, unknown> = {}) {
  const { calls } = mockApi(authedRoutes(routes))
  const view = renderApp("/settings/self-hosted-storage")
  await screen.findByRole("region", { name: "Connection" })
  return { ...view, calls }
}

const connection = () => screen.getByRole("region", { name: "Connection" })
const puts = (calls: { method: string; body: unknown }[]) => calls.filter((c) => c.method === "PUT").map((c) => c.body)
const snippet = () => document.querySelector("#artifact-workflows pre")?.textContent ?? ""

afterEach(() => {
  Reflect.deleteProperty(navigator, "clipboard")
  Reflect.deleteProperty(window, "isSecureContext")
})

describe("Self-hosted storage keys", () => {
  it("shows each key's short ID and when it was set", async () => {
    await draw()
    expect(await within(connection()).findByText("GHRK…7Q2X")).toBeInTheDocument()
    expect(within(connection()).getByText("JOBK…M4TA")).toBeInTheDocument()
    expect(within(connection()).getAllByText("set Oct 1")).toHaveLength(2)
    expect(within(connection()).getByRole("button", { name: "Replace the admin key" })).toBeEnabled()
    expect(within(connection()).getByText(/Needs the token's Variables and Secrets: read and write\./)).toBeInTheDocument()
  })

  it("offers to set a key that is missing", async () => {
    await draw({ "GET /api/artifact-store": { ...fixtures.artifactStore, job_key: undefined } })
    expect(await within(connection()).findByRole("button", { name: "Set the job key" })).toBeInTheDocument()
    expect(within(connection()).getByText("Not set")).toBeInTheDocument()
  })

  it("replaces the job key and keeps no trace of the secret", async () => {
    const { user, calls } = await draw({ "PUT /api/artifact-store/keys": () => noContent() })
    await user.click(await within(connection()).findByRole("button", { name: "Replace the job key" }))
    const dialog = await screen.findByRole("dialog")
    expect(within(dialog).getByText("Replace the job key")).toBeInTheDocument()
    expect(within(dialog).getByLabelText("Secret access key")).toHaveAttribute("type", "password")
    await user.type(within(dialog).getByLabelText("Access key ID"), "JOBKNEWKEY01")
    await user.type(within(dialog).getByLabelText("Secret access key"), "s3cr3t-value")
    await user.click(within(dialog).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(puts(calls)).toEqual([{ which: "job", id: "JOBKNEWKEY01", secret: "s3cr3t-value" }]))
    expect((await screen.findAllByText("Job key saved")).length).toBeGreaterThan(0)
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    expect(screen.queryByDisplayValue("s3cr3t-value")).toBeNull()
  })

  it("asks for both halves of a key before sending", async () => {
    const { user, calls } = await draw({ "PUT /api/artifact-store/keys": () => noContent() })
    await user.click(await within(connection()).findByRole("button", { name: "Replace the admin key" }))
    const dialog = await screen.findByRole("dialog")
    await user.type(within(dialog).getByLabelText("Access key ID"), "GHRKNEWKEY01")
    await user.click(within(dialog).getByRole("button", { name: "Save" }))
    expect(within(dialog).getByRole("alert")).toHaveTextContent("Enter both the access key ID and the secret")
    expect(puts(calls)).toEqual([])
  })

  it("shows the daemon's refusal in the dialog and keeps it open", async () => {
    const { user } = await draw({ "PUT /api/artifact-store/keys": () => json({ error: "the secret must not contain line breaks" }, 400) })
    await user.click(await within(connection()).findByRole("button", { name: "Replace the admin key" }))
    const dialog = await screen.findByRole("dialog")
    await user.type(within(dialog).getByLabelText("Access key ID"), "GHRKNEWKEY01")
    await user.type(within(dialog).getByLabelText("Secret access key"), "x")
    await user.click(within(dialog).getByRole("button", { name: "Save" }))
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("the secret must not contain line breaks")
    expect(screen.getByRole("dialog")).toBeInTheDocument()
  })
})

describe("Self-hosted storage connection check", () => {
  it("lists each check and says how many passed", async () => {
    const checks = [
      { name: "reach", ok: true, detail: "Reached s3.example.sg over HTTPS in 48 ms" },
      { name: "list", ok: true, detail: "Listed gha-artifacts: 186 objects" },
      { name: "head", ok: false, detail: "Could not read the metadata of darkraise/darkmem/102/e2e-report.zip: AccessDenied" },
    ]
    const { user, calls } = await draw({ "POST /api/artifact-store/test": { checks } })
    expect(within(connection()).getByText("Checks use the admin key; a workflow's first upload proves the job key.")).toBeInTheDocument()
    await user.click(within(connection()).getByRole("button", { name: "Test connection" }))
    const list = await within(connection()).findByRole("list", { name: "Connection checks" })
    expect(within(list).getAllByRole("listitem").map((li) => li.textContent)).toEqual([
      "Passed: Reached s3.example.sg over HTTPS in 48 ms",
      "Passed: Listed gha-artifacts: 186 objects",
      "Failed: Could not read the metadata of darkraise/darkmem/102/e2e-report.zip: AccessDenied",
    ])
    expect((await screen.findAllByText("Connection checked: 2 of 3 passed")).length).toBeGreaterThan(0)
    expect(calls.filter((c) => c.path === "/api/artifact-store/test")).toHaveLength(1)
  })
})

describe("Self-hosted storage workflows", () => {
  it("shows the snippet for the running release and copies it", async () => {
    const { user } = await draw()
    const workflows = screen.getByRole("region", { name: "Workflows" })
    expect(snippet()).toContain("GHR_ARTIFACT_STORE: ${{ vars.GHR_ARTIFACT_STORE }}   # empty: GitHub storage")
    expect(snippet()).toContain("- uses: darkraise/ghr/actions/upload@v0.1.26")
    expect(snippet()).toContain("- uses: darkraise/ghr/actions/download@v0.1.26")
    expect(snippet().match(/key: \$\{\{ secrets\.GHR_ARTIFACT_KEY \}\}/g)).toHaveLength(2)
    expect(within(workflows).getByRole("link", { name: "RustFS setup guide: bucket, admin and job key policies" })).toHaveAttribute("href", SETUP_GUIDE)
    Object.defineProperty(window, "isSecureContext", { value: true, configurable: true })
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    await user.click(within(workflows).getByRole("button", { name: "Copy" }))
    expect(writeText).toHaveBeenCalledWith(snippet())
    expect((await screen.findAllByText("Workflow snippet copied")).length).toBeGreaterThan(0)
  })

  it("leaves the tag to fill in on a build that is not a release", async () => {
    await draw({ "GET /api/status": { ...fixtures.status, ghr_update: { installed: "dev", development: true } } })
    expect(snippet()).toContain("- uses: darkraise/ghr/actions/upload@vX.Y.Z")
  })
})
