import { useQueryClient } from "@tanstack/react-query"
import { Link, Outlet, useNavigate } from "@tanstack/react-router"
import { Alert, AlertDescription, AlertTitle } from "darkraise-ui/components/alert"
import { Button } from "darkraise-ui/components/button"
import { SidebarLayout, useBrandStore, type NavGroup } from "darkraise-ui/layout"
import { Boxes, GitBranch, HardDrive, HistoryIcon, LayoutDashboard, LogOut, Settings, Wrench } from "lucide-react"
import { useEffect, useRef } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useEvents, useMetrics, useStatus } from "@/api/hooks"
import type { Config, Status } from "@/api/types"
import { CapacityBar } from "@/components/capacity-bar"
import { ModeControl } from "@/components/mode-control"
import { UpdateCard } from "@/components/update-card"
import { capacity } from "@/lib/capacity"
import { running } from "@/lib/status"
import { errorText } from "@/query"

function count(n: number | undefined): string | undefined {
  return n ? String(n) : undefined
}

function navGroups(status: Status | undefined, config: Config | undefined): NavGroup[] {
  return [
    {
      label: "Operate",
      items: [
        { label: "Dashboard", href: "/", icon: LayoutDashboard },
        { label: "Runners", href: "/runners", icon: Boxes, badge: count(status && running(status)) },
        { label: "History", href: "/history", icon: HistoryIcon },
      ],
    },
    {
      label: "Configure",
      items: [
        { label: "Repositories", href: "/repositories", icon: GitBranch, badge: count(config?.repos?.length) },
        { label: "Toolchains", href: "/toolchains", icon: Wrench },
        { label: "Storage", href: "/storage", icon: HardDrive },
        { label: "Settings", href: "/settings", icon: Settings },
      ],
    },
  ]
}

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

  useEffect(() => {
    useBrandStore.getState().setAppName("ghr")
    // The kit's brand label holds only the app name; ghr-theme.css appends
    // the host from this property.
    document.documentElement.style.setProperty("--ghr-host", JSON.stringify(window.location.hostname))
  }, [])

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
    } catch {
      // a session that already ended still lands on the login page
    }
    queryClient.clear()
    await navigate({ to: "/login", ignoreBlocker: true })
  }

  const st = status.data
  const offline = status.isError
  const unreachable = status.isError && !(status.error instanceof ApiError && status.error.status === 401)
  return (
    <SidebarLayout
      nav={navGroups(st, config.data)}
      showThemeSwitcher={false}
      activeBar="ring"
      notificationSlot={null}
      headerSlot={st && <span className="font-mono text-sm">{capacity(st).text}</span>}
      navHeader={st && <CapacityBar status={st} />}
      navFooter={
        <div className="flex flex-col gap-3">
          {st && (
            <div className="ghr-rail-wide empty:hidden">
              <UpdateCard update={st.runner_update} offline={offline} />
            </div>
          )}
          <div className="ghr-rail-wide flex items-center gap-2 text-sm text-[hsl(var(--sidebar-foreground))]">
            <span className="flex-1 truncate">Owner</span>
            <ModeControl />
            <Button size="sm" variant="ghost" onClick={() => void logout()}>
              <LogOut size={15} aria-hidden="true" />
              Log out
            </Button>
          </div>
        </div>
      }
      user={{ name: "Owner", email: "ghr" }}
      onLogout={() => void logout()}
    >
      {unreachable && (
        <Alert variant="destructive" className="mb-4">
          <AlertTitle>Reconnecting</AlertTitle>
          <AlertDescription>{`Daemon unreachable: ${errorText(status.error)}. Retrying.`}</AlertDescription>
        </Alert>
      )}
      {st?.degraded && (
        <Alert variant="warning" className="mb-4">
          <AlertTitle>Degraded</AlertTitle>
          <AlertDescription>{`${st.degraded_reason ?? "The daemon is degraded"}. No new runners start until this clears.`}</AlertDescription>
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
      <Outlet />
    </SidebarLayout>
  )
}
