import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { keys } from "@/api/hooks"
import type { GitHubQuota, GitHubStorage } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { QuotaCard } from "./quota-card"

function draw(quota: Partial<GitHubQuota> | null, over: Partial<GitHubStorage> = {}) {
  const base = fixtures.githubStorage
  const storage: GitHubStorage = { ...base, quota: quota && base.quota ? { ...base.quota, ...quota } : undefined, ...over }
  const { queryClient, wrapper } = withQuery()
  // useNow reads the daemon's clock from the status, here 2026-10-03.
  queryClient.setQueryData(keys.status, fixtures.status)
  render(<QuotaCard storage={storage} />, { wrapper })
  return screen.getByRole("region", { name: "Quota" })
}

describe("QuotaCard", () => {
  it("shows the billed use against the allowance, with the estimate and projection marked", () => {
    const card = draw({})
    expect(within(card).getByText("72%")).toBeInTheDocument()
    expect(within(card).getByText("261 GB-h of 363 GB-h used in October")).toBeInTheDocument()
    const meter = within(card).getByRole("img", { name: "Used 261 GB-h of 363 GB-h, estimated 263 GB-h, projected 397 GB-h by Nov 1" })
    expect(meter.querySelector("[data-part=used]")).toHaveStyle({ width: "71.7%" })
    expect(meter.querySelector("[data-part=used]")).toHaveClass("bg-warning")
    expect(meter.querySelector("[data-marker=estimate]")).toHaveStyle({ left: "72.4%" })
    expect(meter.querySelector("[data-marker=projected]")).toHaveStyle({ left: "100%" })
    expect(within(card).getByText("Actions 250 GB-h, Packages 10 GB-h")).toBeInTheDocument()
    expect(within(card).getByText("Runs out around Oct 24.")).toBeInTheDocument()
    expect(within(card).getByText("Billing read 13:45")).toBeInTheDocument()
    expect(within(card).getByText(/GitHub recalculates storage every 6 to 12 hours/)).toHaveTextContent(
      "GitHub recalculates storage every 6 to 12 hours, so the estimate adds 12 hours at the current rate. The month is taken to be the calendar month in UTC.",
    )
    expect(within(card).queryByRole("alert")).toBeNull()
  })

  it.each<[string, Partial<GitHubQuota>, string, string]>([
    ["exhausted", { level: "exhausted", used_gb_hours: 373.3, runs_out_at: undefined }, "Uploads fail until Nov 1 unless a budget is set.", "bg-destructive"],
    ["critical past the estimate", { level: "critical", estimated_gb_hours: 370, runs_out_at: undefined }, "Uploads may fail before GitHub's next recalculation.", "bg-destructive"],
    ["calm", { level: "", projected_gb_hours: 300, runs_out_at: undefined }, "Lasts until the month resets on Nov 1.", "bg-primary"],
  ])("words the outlook when %s", (_name, quota, outlook, tint) => {
    const card = draw(quota)
    expect(within(card).getByText(outlook)).toBeInTheDocument()
    expect(within(card).getByRole("img").querySelector("[data-part=used]")).toHaveClass(tint)
  })

  it("shows the billing error and keeps the last quota", () => {
    const error = "the token cannot read darkraise's billing: grant the account permission Plan: read"
    const card = draw({}, { quota_error: error })
    expect(within(card).getByRole("alert")).toHaveTextContent(error)
    expect(within(card).getByText("72%")).toBeInTheDocument()
  })

  it("says when there is no billing read yet", () => {
    const card = draw(null)
    expect(within(card).getByText("No billing read yet.")).toBeInTheDocument()
    expect(within(card).queryByRole("img")).toBeNull()
  })
})
