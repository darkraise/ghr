import type { QueryClient } from "@tanstack/react-query"
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  redirect,
  type RouterHistory,
} from "@tanstack/react-router"
import { api } from "./api/client"
import { keys } from "./api/hooks"
import type { SetupState } from "./api/types"
import { RootLayout } from "./components/root-layout"
import { RouteError } from "./components/route-error"
import { Shell } from "./components/shell"
import { loginSearch, safeRedirect } from "./lib/redirect"
import { DashboardPage } from "./pages/dashboard"
import { HistoryPage } from "./pages/history"
import { LoginPage } from "./pages/login"
import { RepositoriesPage } from "./pages/repositories"
import { RepositoryPage } from "./pages/repository"
import { RunnerDetailPage, type DetailTab } from "./pages/runner-detail"
import { RunnersPage } from "./pages/runners"
import { SettingsPage } from "./pages/settings"
import { SetupPage } from "./pages/setup"
import { StoragePage } from "./pages/storage"
import { ToolchainsPage } from "./pages/toolchains"

// staleTime 0: every navigation into or out of the gated pages asks the
// daemon again, so a logout or an expired session is seen at once.
function authState(queryClient: QueryClient) {
  return queryClient.fetchQuery({ queryKey: keys.auth, queryFn: ({ signal }) => api.authState(signal), staleTime: 0 })
}

async function requireSession(queryClient: QueryClient, href: string) {
  if (!(await authState(queryClient)).authenticated) throw redirect({ to: "/login", search: loginSearch(href) })
}

// staleTime 0, like authState. A failed read does not redirect: the page
// loads, and the Shell's status poll or the wizard reports the daemon.
async function setupState(queryClient: QueryClient): Promise<SetupState | undefined> {
  try {
    return await queryClient.fetchQuery({ queryKey: keys.setup, queryFn: ({ signal }) => api.setupState(signal), staleTime: 0 })
  } catch {
    return undefined
  }
}

const rootRoute = createRootRouteWithContext<{ queryClient: QueryClient }>()({ component: RootLayout })

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  validateSearch: (search: Record<string, unknown>): { redirect?: string } =>
    typeof search.redirect === "string" ? { redirect: search.redirect } : {},
  beforeLoad: async ({ context, search }) => {
    if ((await authState(context.queryClient)).authenticated) throw redirect({ href: safeRedirect(search.redirect) })
  },
  component: LoginPage,
})

const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  beforeLoad: async ({ context, location }) => {
    await requireSession(context.queryClient, location.href)
    if ((await setupState(context.queryClient))?.configured === false) throw redirect({ to: "/setup" })
  },
  component: Shell,
})

// No Shell: its config, metrics and event queries would only meet the setup
// phase's 503.
const setupLayout = createRoute({
  getParentRoute: () => rootRoute,
  id: "setup",
  beforeLoad: async ({ context, location }) => {
    await requireSession(context.queryClient, location.href)
    const state = await setupState(context.queryClient)
    if (state?.configured && !state.setup_pending) throw redirect({ to: "/" })
  },
})

const setupRoute = createRoute({ getParentRoute: () => setupLayout, path: "/setup", component: SetupPage })

const dashboardRoute = createRoute({ getParentRoute: () => appRoute, path: "/", component: DashboardPage })
const repositoriesRoute = createRoute({ getParentRoute: () => appRoute, path: "/repositories", component: RepositoriesPage })
const repositoryRoute = createRoute({ getParentRoute: () => appRoute, path: "/repositories/$name", component: RepositoryPage })
const runnersRoute = createRoute({ getParentRoute: () => appRoute, path: "/runners", component: RunnersPage })
const runnerRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/runners/$id",
  validateSearch: (search: Record<string, unknown>): { tab: DetailTab } => ({
    tab: search.tab === "log" || search.tab === "containers" ? search.tab : "steps",
  }),
  component: RunnerDetailPage,
})
const historyRoute = createRoute({ getParentRoute: () => appRoute, path: "/history", component: HistoryPage })
const toolchainsRoute = createRoute({ getParentRoute: () => appRoute, path: "/toolchains", component: ToolchainsPage })
const storageRoute = createRoute({ getParentRoute: () => appRoute, path: "/storage", component: StoragePage })
const settingsRoute = createRoute({ getParentRoute: () => appRoute, path: "/settings", component: SettingsPage })

const routeTree = rootRoute.addChildren([
  loginRoute,
  setupLayout.addChildren([setupRoute]),
  appRoute.addChildren([
    dashboardRoute,
    repositoriesRoute,
    repositoryRoute,
    runnersRoute,
    runnerRoute,
    historyRoute,
    toolchainsRoute,
    storageRoute,
    settingsRoute,
  ]),
])

export function createAppRouter({ queryClient, history }: { queryClient: QueryClient; history?: RouterHistory }) {
  return createRouter({ routeTree, context: { queryClient }, history, defaultErrorComponent: RouteError })
}

export type AppRouter = ReturnType<typeof createAppRouter>

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter
  }
}
