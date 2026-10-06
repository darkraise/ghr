import { QueryClientProvider, type QueryClient } from "@tanstack/react-query"
import { RouterProvider } from "@tanstack/react-router"
import { Toaster } from "darkraise-ui/components/sonner"
import { ThemeProvider } from "darkraise-ui/theme"
import { useEffect } from "react"
import { setUnauthorizedHandler } from "./api/client"
import type { AppRouter } from "./router"
import { themeConfig } from "./theme.config"

export function App({ router, queryClient }: { router: AppRouter; queryClient: QueryClient }) {
  useEffect(() => {
    // Every poll fails at once when a session expires; one redirect is enough.
    let redirecting = false
    setUnauthorizedHandler(() => {
      const { pathname, href } = router.state.location
      if (redirecting || pathname === "/login") return
      redirecting = true
      queryClient.clear()
      void router
        .navigate({ to: "/login", search: href === "/" ? {} : { redirect: href } })
        .finally(() => {
          redirecting = false
        })
    })
  }, [router, queryClient])
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider config={themeConfig}>
        <RouterProvider router={router} />
        <Toaster />
      </ThemeProvider>
    </QueryClientProvider>
  )
}
