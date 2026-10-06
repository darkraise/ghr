import { useQueryClient } from "@tanstack/react-query"
import { Outlet, useNavigate } from "@tanstack/react-router"
import { Alert, AlertDescription, AlertTitle } from "darkraise-ui/components/alert"
import { SidebarLayout, useBrandStore, type NavGroup } from "darkraise-ui/layout"
import { Boxes, GitBranch, HardDrive, HistoryIcon, LayoutDashboard, Settings } from "lucide-react"
import { useEffect, useRef } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useEvents, useMetrics, useStatus } from "@/api/hooks"
import { errorText } from "@/query"

const nav: NavGroup[] = [
  {
    items: [
      { label: "Dashboard", href: "/", icon: LayoutDashboard },
      { label: "Repositories", href: "/repositories", icon: GitBranch },
      { label: "Runners", href: "/runners", icon: Boxes },
      { label: "History", href: "/history", icon: HistoryIcon },
      { label: "Storage", href: "/storage", icon: HardDrive },
      { label: "Settings", href: "/settings", icon: Settings },
    ],
  },
]

export function Shell() {
  const status = useStatus()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const epoch = status.data?.epoch
  const seenEpoch = useRef<string | undefined>(undefined)
  // Mounted here, not per page, so config, metrics and the event feed poll
  // on every page as the TUI's do, and the Dashboard opens with them warm.
  useConfig()
  useMetrics()
  useEvents(epoch)

  useEffect(() => {
    useBrandStore.getState().setAppName("ghr")
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
    await navigate({ to: "/login" })
  }

  const unreachable = status.isError && !(status.error instanceof ApiError && status.error.status === 401)
  return (
    <SidebarLayout nav={nav} notificationSlot={null} user={{ name: "Owner", email: "ghr" }} onLogout={() => void logout()}>
      {unreachable && (
        <Alert variant="destructive" className="mb-4">
          <AlertTitle>Reconnecting</AlertTitle>
          <AlertDescription>daemon unreachable: {errorText(status.error)} — retrying</AlertDescription>
        </Alert>
      )}
      {status.data?.degraded && (
        <Alert variant="warning" className="mb-4">
          <AlertTitle>Degraded</AlertTitle>
          <AlertDescription>DEGRADED: {status.data.degraded_reason} — no new runners</AlertDescription>
        </Alert>
      )}
      <Outlet />
    </SidebarLayout>
  )
}
