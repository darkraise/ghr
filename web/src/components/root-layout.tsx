import { Outlet } from "@tanstack/react-router"
import { RouterAdapterProvider } from "darkraise-ui/router"
import { routerAdapter } from "@/lib/router-adapter"

export function RootLayout() {
  return (
    <RouterAdapterProvider value={routerAdapter}>
      <Outlet />
    </RouterAdapterProvider>
  )
}
