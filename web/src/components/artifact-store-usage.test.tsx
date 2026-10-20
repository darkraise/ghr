import { screen, within } from "@testing-library/react"
import type { ReactNode } from "react"
import { describe, expect, it } from "vitest"
import { keys } from "@/api/hooks"
import { mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { SETUP_GUIDE } from "./artifact-store-setup"
import { ArtifactStoreEmpty, ArtifactStoreUsage } from "./artifact-store-usage"

function draw(ui: () => ReactNode) {
  mockApi({})
  const view = renderRoutes({ "/": ui, "/settings/$section": () => <p>settings</p> }, "/")
  // useNow reads the daemon's clock from the status, here 2026-10-03 14:05.
  view.queryClient.setQueryData(keys.status, fixtures.status)
  return view
}

describe("ArtifactStoreUsage", () => {
  it("shows use against the capacity, the count, the repositories on and the bucket", async () => {
    draw(() => <ArtifactStoreUsage store={fixtures.artifactStore} />)
    const card = await screen.findByRole("region", { name: "RustFS storage" })
    const used = within(card).getByRole("region", { name: "Used" })
    expect(within(used).getByText("423.8 MB")).toBeInTheDocument()
    expect(within(used).getByText("of 50.0 GB")).toBeInTheDocument()
    expect(within(within(card).getByRole("region", { name: "Artifacts" })).getByText("5")).toBeInTheDocument()
    const on = within(card).getByRole("region", { name: "Repositories using RustFS" })
    expect(within(on).getByText("2")).toBeInTheDocument()
    expect(within(on).getByText("of 3 private")).toBeInTheDocument()
    expect(within(within(card).getByRole("region", { name: "Bucket" })).getByText("s3.example.sg / gha-artifacts")).toBeInTheDocument()
    expect(within(card).getByRole("img", { name: "423.8 MB of 50.0 GB used" })).toBeInTheDocument()
    expect(within(card).getByText("Listed 14:00")).toBeInTheDocument()
    expect(within(card).getByText("Capacity is the size you set in Settings; ghr does not read the VPS disk.")).toBeInTheDocument()
  })

  it("shows the total alone without a capacity", async () => {
    draw(() => <ArtifactStoreUsage store={{ ...fixtures.artifactStore, capacity_bytes: undefined }} />)
    const card = await screen.findByRole("region", { name: "RustFS storage" })
    expect(within(card).queryByRole("img")).toBeNull()
    expect(within(within(card).getByRole("region", { name: "Used" })).queryByText(/^of /)).toBeNull()
    expect(within(card).getByText("Set a capacity in Settings to see use against it; ghr does not read the VPS disk.")).toBeInTheDocument()
  })
})

describe("ArtifactStoreEmpty", () => {
  it("explains the feature and the 7-day default, and leads to setup", async () => {
    draw(() => <ArtifactStoreEmpty />)
    const box = await screen.findByRole("region", { name: "Keep artifacts on your own S3 storage" })
    expect(box).toHaveTextContent("GitHub keeps them 90 days by default, so check workflows that read old artifacts.")
    expect(within(box).getAllByRole("listitem")).toHaveLength(3)
    expect(within(box).getByRole("link", { name: "Set up" })).toHaveAttribute("href", "/settings/self-hosted-storage")
    expect(within(box).getByRole("link", { name: "Read the setup guide" })).toHaveAttribute("href", SETUP_GUIDE)
  })
})
