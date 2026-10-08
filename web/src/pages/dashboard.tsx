import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useActivity, useConfig, useEvents, useMetrics, useStatus, useStorage } from "@/api/hooks"
import { ActivityPanel } from "@/components/activity-panel"
import { AddRepoDialog } from "@/components/add-repo-dialog"
import { DiskBreakdown } from "@/components/disk-breakdown"
import { EventList } from "@/components/event-list"
import { Section } from "@/components/page/section"
import { RepoTable } from "@/components/repo-table"
import { StatCards } from "@/components/stat-card"
import { allPaused, dashboardSummary } from "@/lib/summary"
import { useActivityWindow } from "@/lib/use-activity-window"
import { useNow } from "@/lib/use-now"

export function DashboardPage() {
  const status = useStatus()
  const metrics = useMetrics()
  const config = useConfig()
  const events = useEvents(status.data?.epoch)
  const storage = useStorage(status.data)
  const [selected, select] = useActivityWindow()
  const activity = useActivity(selected)
  const now = useNow()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [adding, setAdding] = useState(false)
  const toggle = useMutation({
    mutationFn: (resume: boolean) => (resume ? api.resumeAll() : api.pauseAll()),
    onSuccess: (_data, resume) => {
      toast.success(resume ? "Resumed all repositories" : "Paused all repositories. Running jobs finish first.")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.status }),
  })

  const st = status.data
  if (!st) {
    return (
      <div className="flex flex-col gap-4">
        <PageHeader title="Dashboard" />
        <Spinner label="Waiting for the daemon" />
      </div>
    )
  }
  const offline = status.isError
  const paused = allPaused(st.repos)
  const configured = st.repos.filter((r) => !r.removing).length
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Dashboard"
        description={dashboardSummary(st)}
        actions={
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" disabled={offline || toggle.isPending} onClick={() => toggle.mutate(paused)}>
              {paused ? "Resume all" : "Pause all"}
            </Button>
            <Button disabled={offline} onClick={() => setAdding(true)}>
              Add repository
            </Button>
          </div>
        }
      />
      <StatCards status={st} metrics={metrics.data} now={now} />
      <ActivityPanel
        selected={selected}
        onSelect={select}
        activity={activity.data}
        error={activity.error}
        onRetry={() => void activity.refetch()}
        now={now}
        onOpenRunner={(id) => void navigate({ to: "/runners/$id", params: { id }, search: { tab: "steps" } })}
      />
      <div className="grid gap-4 xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <Section
          title="Repositories"
          aside={
            <>
              <span className="text-sm text-muted-foreground">{configured} configured</span>
              <Link to="/repositories" className="ml-auto text-sm text-primary hover:underline">
                Manage
              </Link>
            </>
          }
        >
          {st.repos.length === 0 ? (
            <div className="flex flex-col items-start gap-2 py-2">
              <p className="text-sm text-muted-foreground">No repositories yet</p>
              <Button size="sm" disabled={offline} onClick={() => setAdding(true)}>
                Add repository
              </Button>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <RepoTable
                repos={st.repos}
                activity={activity.data?.repos}
                now={now}
                offline={offline}
                onOpen={(name) => void navigate({ to: "/repositories/$name", params: { name } })}
              />
            </div>
          )}
        </Section>
        <div className="flex min-w-0 flex-col gap-4">
          <Section
            title="Disk"
            aside={
              <>
                {st.disk_root && <span className="font-mono text-sm text-muted-foreground">{st.disk_root}</span>}
                <Link to="/storage" className="ml-auto text-sm text-primary hover:underline">
                  Storage
                </Link>
              </>
            }
          >
            <DiskBreakdown status={st} storage={storage.data} highWater={config.data?.disk_high_water ?? 80} />
          </Section>
          <Section title="Events">
            <EventList events={events} />
          </Section>
        </div>
      </div>
      <AddRepoDialog open={adding} onClose={() => setAdding(false)} />
    </div>
  )
}
