import { useNavigate, useParams, useSearch } from "@tanstack/react-router"
import { PageHeader } from "darkraise-ui/layout"
import { useEffect } from "react"
import { useStatus } from "@/api/hooks"
import { SplitView } from "@/components/page/split-view"
import { RunnerList } from "@/components/runner-list"
import { RunnerPanel, type DetailTab } from "@/components/runner-panel"
import { runnersSummary } from "@/lib/summary"
import { useMediaQuery, WIDE } from "@/lib/use-media-query"
import { useNow } from "@/lib/use-now"

function RunnersView({ id, tab }: { id: string | undefined; tab: DetailTab }) {
  const status = useStatus()
  const now = useNow()
  const wide = useMediaQuery(WIDE)
  const navigate = useNavigate()
  const first = status.data?.instances.find((i) => i.state !== "cleaning")?.id

  // On wide screens the first live runner goes into the URL, so the selection
  // stays put when that runner finishes. Narrow screens keep the list.
  useEffect(() => {
    if (wide && id === undefined && first !== undefined) {
      void navigate({ to: "/runners/$id", params: { id: first }, search: { tab: "steps" }, replace: true })
    }
  }, [wide, id, first, navigate])

  const list = <RunnerList status={status.data} selected={id} tab={tab} now={now} />
  const panel = id === undefined ? null : <RunnerPanel key={id} id={id} tab={tab} backLink={!wide} />
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title="Runners" description={status.data ? runnersSummary(status.data) : undefined} />
      <SplitView list={list} panel={panel} narrow={panel ?? list} />
    </div>
  )
}

export function RunnersPage() {
  return <RunnersView id={undefined} tab="steps" />
}

export function RunnerPage() {
  const { id } = useParams({ from: "/app/runners/$id" })
  const { tab } = useSearch({ from: "/app/runners/$id" })
  return <RunnersView id={id} tab={tab} />
}
