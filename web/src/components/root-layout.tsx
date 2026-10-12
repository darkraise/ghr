import { Outlet } from "@tanstack/react-router"
import { RouterAdapterProvider } from "darkraise-ui/router"
import { RestartScreen } from "@/components/restart-screen"
import { routerAdapter } from "@/lib/router-adapter"
import { useRestarting } from "@/lib/restarting"

// The restarting screen takes the place of every route, outside the app
// route and its guards, so nothing the restart ends can redirect the page.
export function RootLayout() {
  const restarting = useRestarting()
  return (
    <RouterAdapterProvider value={routerAdapter}>
      {restarting ? <RestartScreen url={restarting.url} same={restarting.same} /> : <Outlet />}
    </RouterAdapterProvider>
  )
}
