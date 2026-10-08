import { QueryClientProvider } from "@tanstack/react-query"
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router"
import { render } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { Toaster } from "darkraise-ui/components/sonner"
import { TooltipProvider } from "darkraise-ui/components/tooltip"
import type { ReactNode } from "react"
import { createQueryClient } from "@/query"

// Renders components that need the router outside the app's route tree. Each
// key is a path pattern; its view may read params with useParams, and every
// route keeps its search as given.
export function renderRoutes(paths: Record<string, () => ReactNode>, initial: string) {
  const queryClient = createQueryClient()
  const root = createRootRoute({ component: () => <Outlet /> })
  const routes = Object.entries(paths).map(([path, view]) =>
    createRoute({
      getParentRoute: () => root,
      path,
      validateSearch: (search: Record<string, unknown>) => search,
      component: () => <>{view()}</>,
    }),
  )
  const router = createRouter({ routeTree: root.addChildren(routes), history: createMemoryHistory({ initialEntries: [initial] }) })
  const user = userEvent.setup()
  const view = render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RouterProvider router={router} />
        <Toaster />
      </TooltipProvider>
    </QueryClientProvider>,
  )
  return { ...view, router, queryClient, user }
}
