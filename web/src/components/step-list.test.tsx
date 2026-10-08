import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Step } from "@/api/types"
import { StepList } from "./step-list"

const t = (hms: string) => `2026-10-03T${hms}Z`

describe("StepList", () => {
  it("names each step's state and draws its time on the axis", () => {
    const steps: Step[] = [
      { number: 1, name: "checkout", status: "completed", conclusion: "success", started_at: t("14:00:00"), completed_at: t("14:01:00") },
      { number: 2, name: "lint", status: "completed", conclusion: "neutral", started_at: t("14:01:00"), completed_at: t("14:01:30") },
      { number: 3, name: "test", status: "in_progress", conclusion: "", started_at: t("14:01:30") },
      { number: 4, name: "upload", status: "queued", conclusion: "" },
    ]
    const { container } = render(<StepList steps={steps} now={Date.parse(t("14:03:00"))} live />)
    expect(screen.getByRole("list", { name: "Steps" })).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "Succeeded" })).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "Failed" })).toBeInTheDocument()
    expect(screen.getByText("Running")).toHaveClass("sr-only")
    expect(screen.getByRole("img", { name: "Pending" })).toBeInTheDocument()
    expect(screen.getByText("upload")).toHaveClass("text-muted-foreground")
    expect(screen.getByText("1m00s")).toBeInTheDocument()
    expect(screen.getByText("1m30s")).toBeInTheDocument()
    const bars = container.querySelectorAll<HTMLElement>("[data-bar]")
    expect(bars).toHaveLength(3)
    expect(bars[0]?.style.left).toBe("0%")
    expect(bars[2]?.style.left).toBe("50%")
  })
})
