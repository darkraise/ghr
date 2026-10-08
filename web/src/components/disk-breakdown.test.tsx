import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { diskParts } from "@/lib/disk"
import { fixtures } from "@/test/fixtures"
import { DiskBreakdown } from "./disk-breakdown"

describe("diskParts", () => {
  it("splits the used bytes into what fills the disk", () => {
    expect(diskParts(fixtures.storage, 146_000_000_000)).toEqual([
      { key: "build-cache", label: "Build cache", bytes: 420_000_000 },
      { key: "images", label: "Images", bytes: 8_100_000_000 },
      { key: "containers", label: "Containers", bytes: 0 },
      { key: "volumes", label: "Local volumes", bytes: 0 },
      { key: "package-caches", label: "Package caches", bytes: 3_600_000_000 },
      { key: "toolchains", label: "Toolchains", bytes: 270_000_000 },
      { key: "other", label: "Other", bytes: 133_610_000_000 },
    ])
  })

  it("clamps other at zero", () => {
    expect(diskParts(fixtures.storage, 1_000).at(-1)?.bytes).toBe(0)
  })

  it("shows one used part until storage is measured", () => {
    expect(diskParts(undefined, 5)).toEqual([{ key: "used", label: "Used", bytes: 5 }])
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

  it("says when the disk is not measured yet", () => {
    render(<DiskBreakdown status={{ ...fixtures.status, disk_total_bytes: 0 }} storage={undefined} highWater={80} />)
    expect(screen.getByText("Not measured yet")).toBeInTheDocument()
  })
})
