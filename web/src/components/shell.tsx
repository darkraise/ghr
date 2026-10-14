import { useQueryClient } from "@tanstack/react-query"
import { Link, Outlet, useNavigate } from "@tanstack/react-router"
import { Alert, AlertDescription, AlertTitle } from "darkraise-ui/components/alert"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Archive, Boxes, GitBranch, HistoryIcon, LayoutDashboard, LogOut, Settings, Timer, Workflow, type LucideIcon } from "lucide-react"
import { useEffect, useRef } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useEvents, useMetrics, useStatus } from "@/api/hooks"
import { useChangeStream } from "@/api/use-change-stream"
import type { Config, Status } from "@/api/types"
import { Brand } from "@/components/brand"
import { CapacitySegments } from "@/components/capacity-bar"
import { ModeControl } from "@/components/mode-control"
import { QuotaBanner } from "@/components/quota-banner"
import { ListenErrorBanner, RestartBanner } from "@/components/restart-banner"
import { UpdateCard } from "@/components/update-card"
import { capacity } from "@/lib/capacity"
import { running } from "@/lib/status"
import { errorText } from "@/query"

function count(n: number | undefined): string | undefined {
  return n ? String(n) : undefined
}

interface Tab {
  label: string
  to: "/" | "/runners" | "/actions" | "/history" | "/jobs" | "/artifacts" | "/repositories" | "/settings"
  icon: LucideIcon
  badge?: string
  divided?: boolean
}

function tabs(status: Status | undefined, config: Config | undefined): Tab[] {
  return [
    { label: "Dashboard", to: "/", icon: LayoutDashboard },
    { label: "Runners", to: "/runners", icon: Boxes, badge: count(status && running(status)) },
    { label: "Actions", to: "/actions", icon: Workflow },
    { label: "History", to: "/history", icon: HistoryIcon },
    { label: "Jobs", to: "/jobs", icon: Timer },
    { label: "Artifacts", to: "/artifacts", icon: Archive },
    { label: "Repositories", to: "/repositories", icon: GitBranch, badge: count(config?.repos?.length), divided: true },
    { label: "Settings", to: "/settings", icon: Settings },
  ]
}

const TAB =
  "flex shrink-0 items-center gap-2 rounded-t-md border-b-2 border-transparent px-3 py-2 text-sm whitespace-nowrap hover:bg-muted aria-[current=page]:border-[hsl(var(--tab-line))] focus-ring-tight aria-[current=page]:font-semibold"
const FRAME = "mx-auto w-full max-w-[1264px] px-6"

export function Shell() {
  const status = useStatus()
  const config = useConfig()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const epoch = status.data?.epoch
  const seenEpoch = useRef<string | undefined>(undefined)
  // Mounted here, not per page, so config, metrics and the event feed poll
  // on every page, and the Dashboard opens with them warm.
  useMetrics()
  useEvents(epoch)
  useChangeStream()

  useEffect(() => {
    if (epoch === undefined) return
    if (seenEpoch.current !== undefined && seenEpoch.current !== epoch) {
      void queryClient.invalidateQueries({ queryKey: keys.config })
    }
    seenEpoch.current = epoch
  }, [epoch, queryClient])

  async function logout() {
    try {
      await api.logout()
    } catch (err) {
      // 401: the session had already ended, which is the outcome asked for.
      if (!(err instanceof ApiError && err.status === 401)) {
        toast.error(`Not logged out: ${errorText(err)}`)
        return
      }
    }
    queryClient.clear()
    await navigate({ to: "/login", ignoreBlocker: true })
  }

  const st = status.data
  const offline = status.isError
  const unreachable = status.isError && !(status.error instanceof ApiError && status.error.status === 401)
  return (
    <div className="flex min-h-dvh flex-col bg-background text-foreground">
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-50 focus:rounded-md focus:bg-card focus:px-3 focus:py-2 focus:text-sm"
      >
        Skip to content
      </a>
      <header className="border-b border-border bg-[hsl(var(--top-bar))]">
        <div className={`${FRAME} flex h-14 items-center gap-3`}>
          <Link to="/" aria-label="ghr" className="flex items-center rounded-md">
            <Brand />
          </Link>
          <span className="hidden font-mono text-xs text-muted-foreground sm:inline">{window.location.hostname}</span>
          <span className="flex-1" />
          {st && (
            <span className="flex items-center gap-2">
              <span className="w-20">
                <CapacitySegments status={st} />
              </span>
              <span className="hidden font-mono text-sm sm:inline">{capacity(st).text}</span>
            </span>
          )}
          <ModeControl />
          <Button size="sm" variant="ghost" aria-label="Log out" onClick={() => void logout()}>
            <LogOut size={15} aria-hidden="true" />
            <span className="hidden sm:inline">Log out</span>
          </Button>
        </div>
        {/* relative: the tabs' sr-only separators are absolutely positioned, and
            only a positioned scroller clips them. */}
        <nav aria-label="Pages" className={`${FRAME} relative flex gap-1 overflow-x-auto`}>
          {tabs(st, config.data).map((tab) => (
            <span key={tab.to} className={`flex shrink-0 ${tab.divided ? "ml-2 border-l border-border pl-2" : ""}`}>
              <Link to={tab.to} activeOptions={{ exact: tab.to === "/" }} className={TAB}>
                <tab.icon size={15} aria-hidden="true" />
                {tab.label}
                {tab.badge && (
                  <>
                    <span className="sr-only">, </span>
                    <span className="rounded-full bg-muted px-1.5 font-mono text-xs">{tab.badge}</span>
                  </>
                )}
              </Link>
            </span>
          ))}
        </nav>
      </header>
      <main id="main-content" tabIndex={-1} className={`${FRAME} flex flex-1 flex-col py-6 outline-none`}>
      {unreachable && (
        <Alert variant="destructive" className="mb-4">
          <AlertTitle>Reconnecting</AlertTitle>
          <AlertDescription>{`Daemon unreachable: ${errorText(status.error)}. Retrying.`}</AlertDescription>
        </Alert>
      )}
      {st?.degraded && (
        <Alert variant="warning" className="mb-4">
          <AlertTitle>Degraded</AlertTitle>
          <AlertDescription>{`${(st.degraded_reason ?? "The daemon is degraded").replace(/\.$/, "")}. No new runners start until this clears.`}</AlertDescription>
        </Alert>
      )}
      {st?.setup_pending && (
        <Alert className="mb-4">
          <AlertTitle>First-run setup is not finished.</AlertTitle>
          <AlertDescription>
            <Link to="/setup" className="underline">
              Finish setup
            </Link>
          </AlertDescription>
        </Alert>
      )}
        {st && (
          <div className="mb-4 flex flex-col gap-2 empty:hidden">
            {st.web_listen_error ? <ListenErrorBanner error={st.web_listen_error} /> : <RestartBanner status={st} offline={offline} />}
            {st.github_storage && <QuotaBanner alert={st.github_storage} />}
            <UpdateCard update={st.runner_update} offline={offline} />
          </div>
        )}
        <Outlet />
      </main>
    </div>
  )
}
