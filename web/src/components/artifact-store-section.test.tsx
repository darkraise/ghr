import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { type Call, json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

async function draw(routes: Record<string, unknown> = {}) {
  const { calls } = mockApi(authedRoutes({ "PATCH /api/config": () => noContent(), ...routes }))
  const view = renderApp("/settings/self-hosted-storage")
  await screen.findByRole("region", { name: "Connection" })
  return { ...view, calls }
}

const box = (name: string) => screen.getByRole("textbox", { name }) as HTMLInputElement
const capacity = () => screen.getByRole("spinbutton", { name: "Capacity" }) as HTMLInputElement
const patches = (calls: Call[]) => calls.filter((c) => c.method === "PATCH")
// The alerts on the page itself, not the toasts that repeat them.
const pageAlerts = () =>
  screen
    .queryAllByRole("alert")
    .filter((a) => !a.closest("[data-sonner-toaster]"))
    .map((a) => a.textContent)

const notSetUp = { "GET /api/config": { ...fixtures.config, artifact_store: undefined } }

describe("Self-hosted storage section", () => {
  it("shows the saved connection and policy", async () => {
    await draw()
    expect(screen.getByRole("heading", { level: 1, name: "Self-hosted storage" })).toBeInTheDocument()
    expect(screen.getByText("Saved in config.yaml as artifact_store; the keys are kept in /etc/ghr/artifact-store-keys.")).toBeInTheDocument()
    expect(box("Endpoint").value).toBe("https://s3.example.sg")
    expect(box("Bucket").value).toBe("gha-artifacts")
    expect(box("Region").value).toBe("us-east-1")
    expect(within(screen.getByRole("region", { name: "Policy" })).getByRole("textbox", { name: "Cleanup age" })).toHaveValue("7d")
    expect(capacity().value).toBe("50")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("saves only the changed fields", async () => {
    const { user, calls } = await draw()
    await user.clear(box("Bucket"))
    await user.type(box("Bucket"), "ci-artifacts")
    await user.clear(capacity())
    await user.type(capacity(), "80")
    expect(screen.getByText("2 unsaved changes")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(patches(calls).map((c) => c.body)).toEqual([{ artifact_store: { bucket: "ci-artifacts", capacity_gb: 80 } }]))
    expect((await screen.findAllByText("Self-hosted storage saved")).length).toBeGreaterThan(0)
  })

  it("starts from empty fields before setup and sends what was typed", async () => {
    const { user, calls } = await draw(notSetUp)
    expect(box("Endpoint").value).toBe("")
    expect(box("Region")).toHaveAttribute("placeholder", "us-east-1")
    expect(capacity().value).toBe("")
    await user.type(box("Endpoint"), "https://s3.example.sg")
    await user.type(box("Bucket"), "gha-artifacts")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() =>
      expect(patches(calls).map((c) => c.body)).toEqual([{ artifact_store: { endpoint: "https://s3.example.sg", bucket: "gha-artifacts" } }]),
    )
  })

  it.each<[string, string, string]>([
    ["Endpoint", "http://s3.example.sg", "Enter an https:// address; ghr refuses plain http"],
    ["Endpoint", "s3.example.sg", "Start the address with https://"],
  ])("refuses %s %s before sending", async (name, value, error) => {
    const { user, calls } = await draw()
    await user.clear(box(name))
    await user.type(box(name), value)
    expect(await screen.findByText(error)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Fix the highlighted settings first")).length).toBeGreaterThan(0)
    expect(patches(calls)).toHaveLength(0)
  })

  it("asks for the bucket once an endpoint is typed", async () => {
    const { user, calls } = await draw(notSetUp)
    await user.type(box("Endpoint"), "https://s3.example.sg")
    expect(await screen.findByText("Enter the bucket the endpoint holds")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Fix the highlighted settings first")).length).toBeGreaterThan(0)
    expect(patches(calls)).toHaveLength(0)
  })

  it("shows the daemon's refusal and keeps the edit", async () => {
    const msg = "artifact_store.bucket must be a valid S3 bucket name"
    const { user } = await draw({ "PATCH /api/config": () => json({ error: msg }, 400) })
    await user.clear(box("Bucket"))
    await user.type(box("Bucket"), "Bad_Bucket")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(pageAlerts()).toContain(msg))
    expect(box("Bucket").value).toBe("Bad_Bucket")
  })

  it("asks before leaving with unsaved changes", async () => {
    const { user, router } = await draw()
    await user.clear(box("Region"))
    await user.type(box("Region"), "ap-southeast-1")
    await user.click(screen.getAllByRole("link", { name: "Dashboard" })[0] as HTMLElement)
    expect(await screen.findByText("You have 1 unsaved change on the Self-hosted storage page.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Stay" }))
    expect(router.state.location.pathname).toBe("/settings/self-hosted-storage")
  })
})
