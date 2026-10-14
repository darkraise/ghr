import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { diskParts, otherFilesystemParts } from "@/lib/disk"
import { fixtures } from "@/test/fixtures"
import { DiskBar, DiskBreakdown } from "./disk-breakdown"

describe("diskParts", () => {
  it("splits the used bytes into what fills the disk", () => {
    expect(diskParts(fixtures.storage, 146_000_000_000)).toEqual([
      { key: "build-cache", docker: "Build Cache", label: "Build cache", bytes: 420_000_000 },
      { key: "images", docker: "Images", label: "Images", bytes: 8_100_000_000 },
      { key: "containers", docker: "Containers", label: "Containers", bytes: 0 },
      { key: "volumes", docker: "Local Volumes", label: "Local volumes", bytes: 0 },
      { key: "package-caches", label: "Package caches", bytes: 3_600_000_000 },
      { key: "toolchains", label: "Toolchains", bytes: 270_000_000 },
      { key: "other", label: "Other", bytes: 133_610_000_000 },
    ])
  })

  it("leaves the caches out when they are on another filesystem", () => {
    const apart = { ...fixtures.storage, caches_on_data_root: false }
    expect(diskParts(apart, 146_000_000_000)).toEqual([
      { key: "build-cache", docker: "Build Cache", label: "Build cache", bytes: 420_000_000 },
      { key: "images", docker: "Images", label: "Images", bytes: 8_100_000_000 },
      { key: "containers", docker: "Containers", label: "Containers", bytes: 0 },
      { key: "volumes", docker: "Local Volumes", label: "Local volumes", bytes: 0 },
      { key: "other", label: "Other", bytes: 137_480_000_000 },
    ])
  })

  it("clamps other at zero", () => {
    expect(diskParts(fixtures.storage, 1_000).at(-1)?.bytes).toBe(0)
  })

  it("shows one used part until storage is measured", () => {
    expect(diskParts(undefined, 5)).toEqual([{ key: "used", label: "Used", bytes: 5 }])
  })
})

describe("otherFilesystemParts", () => {
  it("lists the caches only when they are on another filesystem", () => {
    expect(otherFilesystemParts({ ...fixtures.storage, caches_on_data_root: false })).toEqual([
      { key: "package-caches", label: "Package caches", bytes: 3_600_000_000 },
      { key: "toolchains", label: "Toolchains", bytes: 270_000_000 },
    ])
    expect(otherFilesystemParts(fixtures.storage)).toEqual([])
    expect(otherFilesystemParts(undefined)).toEqual([])
  })
})

describe("DiskBreakdown", () => {
  it("shows the headline, the stacked bar, the prune marker and the legend", () => {
    const { container } = render(<DiskBreakdown status={fixtures.status} storage={fixtures.storage} highWater={80} />)
    expect(screen.getByText("61%")).toHaveClass("font-mono")
    expect(screen.getByText("146.0 GB of 240.0 GB used, prunes at 80%")).toBeInTheDocument()
    expect(container.querySelector('[data-marker="high-water"]')).toHaveStyle({ left: "80%" })
    expect(container.querySelector('[data-part="images"]')).toHaveStyle({ width: `${(8_100_000_000 / 240_000_000_000) * 100}%` })
    const legend = within(screen.getByRole("list"))
    expect(legend.getByText("Images")).toBeInTheDocument()
    expect(legend.getByText("8.1 GB")).toBeInTheDocument()
    expect(legend.getByText("133.6 GB")).toBeInTheDocument()
  })

  it("never paints a category in a status colour", () => {
    const { container } = render(<DiskBreakdown status={fixtures.status} storage={fixtures.storage} highWater={80} />)
    for (const part of container.querySelectorAll("[data-part]")) expect(part.className).not.toMatch(/success|destructive|warning/)
  })

  it("lists the caches under another filesystem and keeps them out of the bar", () => {
    const { container } = render(
      <DiskBreakdown status={fixtures.status} storage={{ ...fixtures.storage, caches_on_data_root: false }} highWater={80} />,
    )
    expect(container.querySelector('[data-part="toolchains"]')).toBeNull()
    expect(container.querySelector('[data-part="package-caches"]')).toBeNull()
    expect(screen.getByRole("img", { name: /^Disk use: .*Other 137\.5 GB$/ })).toBeInTheDocument()
    expect(screen.queryByRole("img", { name: /Toolchains|Package caches/ })).toBeNull()
    const lists = screen.getAllByRole("list")
    const legend = within(lists[0] as HTMLElement)
    const apart = within(lists[1] as HTMLElement)
    expect(lists).toHaveLength(2)
    expect(legend.queryByText("Toolchains")).toBeNull()
    expect(legend.getByText("Images")).toBeInTheDocument()
    expect(screen.getByText("On another filesystem")).toBeInTheDocument()
    expect(apart.getByText("Toolchains")).toBeInTheDocument()
    expect(apart.getByText("270.0 MB")).toBeInTheDocument()
    expect(apart.getByText("Package caches")).toBeInTheDocument()
    expect(apart.getByText("3.6 GB")).toBeInTheDocument()
  })

  it("has no other filesystem line when the caches share the disk", () => {
    render(<DiskBreakdown status={fixtures.status} storage={fixtures.storage} highWater={80} />)
    expect(screen.queryByText("On another filesystem")).toBeNull()
  })

  it("says when the disk is not measured yet", () => {
    render(<DiskBreakdown status={{ ...fixtures.status, disk_total_bytes: 0 }} storage={undefined} highWater={80} />)
    expect(screen.getByText("Not measured yet")).toBeInTheDocument()
  })
})

describe("DiskBar", () => {
  it("draws the bar and the prune marker without a legend", () => {
    const { container } = render(<DiskBar status={fixtures.status} storage={fixtures.storage} highWater={85} />)
    expect(screen.getByRole("img", { name: /^Disk use: Build cache 420\.0 MB, Images 8\.1 GB/ })).toBeInTheDocument()
    expect(container.querySelector('[data-marker="high-water"]')).toHaveStyle({ left: "85%" })
    expect(screen.queryByRole("list")).toBeNull()
  })
})
