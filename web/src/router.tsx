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
import type { DetailTab } from "./components/runner-panel"
import { Shell } from "./components/shell"
import { parseActionsSearch, type ActionsSearch } from "./lib/actions"
import { parseHistorySearch, type HistorySearch } from "./lib/history"
import { loginSearch, safeRedirect } from "./lib/redirect"
import { parseJobSearch, parseRunSearch, type JobSearch, type RunSearch } from "./lib/run-details"
import { ActionsPage } from "./pages/actions"
import { DashboardPage } from "./pages/dashboard"
import { HistoryPage } from "./pages/history"
import { LoginPage } from "./pages/login"
import { RepositoriesPage } from "./pages/repositories"
import { RepositoryPage } from "./pages/repository"
import { JobPage, RunNotFound, RunPage } from "./pages/run"
import { RunnerPage, RunnersPage } from "./pages/runners"
import { SettingsPage } from "./pages/settings"
import { SetupPage } from "./pages/setup"

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
  component: RunnerPage,
})
const historyRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/history",
  // The router lays a child's validated search over its parent's raw one, so a
  // value the parser dropped would come back; naming the keys overrides it.
  validateSearch: (search: Record<string, unknown>): HistorySearch => ({
    repo: undefined,
    result: undefined,
    window: undefined,
    ...parseHistorySearch(search),
  }),
  component: HistoryPage,
})
const actionsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/actions",
  // As History: naming the keys keeps a value the parser dropped from coming back.
  validateSearch: (search: Record<string, unknown>): ActionsSearch => ({
    repo: undefined,
    status: undefined,
    ...parseActionsSearch(search),
  }),
  component: ActionsPage,
})
// A value the parser refused fails the route, so a bad attempt, step or line
// shows the not-found line instead of quietly falling back to the latest
// attempt; the router would otherwise drop it from the address.
function strictSearch<T extends object>(raw: Record<string, unknown>, parsed: T, names: string[]): T {
  const kept = new Map(Object.entries(parsed))
  for (const name of names) {
    if (raw[name] !== undefined && kept.get(name) === undefined) throw new Error(`${name} must be a positive integer`)
  }
  return parsed
}

const runRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/actions/$repo/runs/$id",
  validateSearch: (search: Record<string, unknown>): RunSearch => ({
    attempt: undefined,
    ...strictSearch(search, parseRunSearch(search), ["attempt"]),
  }),
  component: RunPage,
  errorComponent: RunNotFound,
})
const jobRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/actions/$repo/runs/$id/jobs/$jobId",
  validateSearch: (search: Record<string, unknown>): JobSearch => ({
    attempt: undefined,
    step: undefined,
    line: undefined,
    ...strictSearch(search, parseJobSearch(search), ["attempt", "step", "line"]),
  }),
  component: JobPage,
  errorComponent: RunNotFound,
})
const toolchainsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/toolchains",
  beforeLoad: ({ location }) => {
    throw redirect({ to: "/settings/$section", params: { section: "toolchains" }, hash: location.hash, replace: true })
  },
})
const storageRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/storage",
  beforeLoad: ({ location }) => {
    throw redirect({ to: "/settings/$section", params: { section: "storage" }, hash: location.hash, replace: true })
  },
})
const settingsRoute = createRoute({ getParentRoute: () => appRoute, path: "/settings", component: SettingsPage })
// The section pages read $section themselves, so the route table does not
// change when a page gains or loses a section.
const settingsSectionRoute = createRoute({ getParentRoute: () => appRoute, path: "/settings/$section", component: SettingsPage })
const repositorySectionRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/repositories/$name/$section",
  component: RepositoryPage,
})

const routeTree = rootRoute.addChildren([
  loginRoute,
  setupLayout.addChildren([setupRoute]),
  appRoute.addChildren([
    dashboardRoute,
    repositoriesRoute,
    repositoryRoute,
    repositorySectionRoute,
    runnersRoute,
    runnerRoute,
    actionsRoute,
    runRoute,
    jobRoute,
    historyRoute,
    toolchainsRoute,
    storageRoute,
    settingsRoute,
    settingsSectionRoute,
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
