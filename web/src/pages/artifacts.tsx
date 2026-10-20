import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate, useSearch } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "darkraise-ui/components/tabs"
import { PageHeader } from "darkraise-ui/layout"
import { api } from "@/api/client"
import { keys, useArtifactStore, useConfig, useGitHubStorage, useStatus } from "@/api/hooks"
import type { Config } from "@/api/types"
import { ArtifactRepoTable } from "@/components/artifact-repo-table"
import { ArtifactStoreTable } from "@/components/artifact-store-table"
import { ArtifactStoreEmpty, ArtifactStoreUsage } from "@/components/artifact-store-usage"
import { ErrorLine } from "@/components/page/error-line"
import { QuotaCard } from "@/components/quota-card"
import { OperationProgress, RecentDeletions } from "@/components/storage-operations"
import { mergeDeletions, selfHostedSummary } from "@/lib/artifact-store"
import { shortWhen } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

type Tab = "github" | "self-hosted"

function policySummary(config: Config | undefined): string {
  if (!config) return ""
  const g = config.github_storage
  const [warn, critical] = g?.warn_pct ?? [70, 90]
  const n = Object.keys(g?.repos ?? {}).length
  const overrides = n === 0 ? "" : ` (${n} repository override${n === 1 ? "" : "s"})`
  return ` · cleanup ${g?.cleanup_max_age || "off"}${overrides} · warnings at ${warn}% and ${critical}%`
}

function TabCount({ n }: { n: number | undefined }) {
  if (n === undefined) return null
  return (
    <>
      {" "}
      <span className="font-mono text-xs text-muted-foreground">{n}</span>
    </>
  )
}

export function ArtifactsPage() {
  const search = useSearch({ from: "/app/artifacts" })
  const navigate = useNavigate()
  const tab: Tab = search.store === "self-hosted" ? "self-hosted" : "github"
  const storage = useGitHubStorage()
  const selfHosted = useArtifactStore()
  const status = useStatus()
  const config = useConfig()
  const queryClient = useQueryClient()
  const now = useNow()
  const refreshGitHub = useMutation({
    mutationFn: () => api.refreshGitHubStorage(),
    onSuccess: () => {
      toast.success("Check started")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.githubStorage }),
  })
  const refreshSelfHosted = useMutation({
    mutationFn: () => api.refreshArtifactStore(),
    onSuccess: () => {
      toast.success("Check started")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.artifactStore }),
  })
  const data = storage.data
  const store = selfHosted.data
  const configured = store?.configured === true
  const offline = status.isError
  const checked = data ? (data.checked_at ? `Checked ${shortWhen(data.checked_at, now)}` : "Not checked yet") : undefined
  const description = tab === "github" ? checked && `${checked}${policySummary(config.data)}` : selfHostedSummary(store, config.data, now)
  const refresh = tab === "github" ? refreshGitHub : refreshSelfHosted
  const githubCount = data?.repos.reduce((n, r) => n + r.artifact_count, 0)

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Artifacts"
        description={description}
        actions={
          <>
            <Button variant="outline" asChild>
              <Link to="/settings/$section" params={{ section: tab === "github" ? "github-storage" : "self-hosted-storage" }}>
                Policy
              </Link>
            </Button>
            <Button
              variant="outline"
              disabled={offline || refresh.isPending || (tab === "self-hosted" && !configured)}
              onClick={() => refresh.mutate()}
            >
              Check now
            </Button>
          </>
        }
      />
      <Tabs value={tab} onValueChange={(t) => void navigate({ to: "/artifacts", search: t === "self-hosted" ? { store: "self-hosted" } : {} })}>
        <TabsList>
          <TabsTrigger value="github">
            GitHub
            <TabCount n={githubCount} />
          </TabsTrigger>
          <TabsTrigger value="self-hosted">
            Self-hosted
            <TabCount n={store?.configured ? store.count : undefined} />
          </TabsTrigger>
        </TabsList>
        <TabsContent value="github" className="flex flex-col gap-4">
          {storage.isError && <ErrorLine onRetry={() => void storage.refetch()}>{errorText(storage.error)}</ErrorLine>}
          {data?.error && <ErrorLine>{`Could not list the repositories: ${data.error}`}</ErrorLine>}
          {!data && !storage.isError && <Spinner label="Loading" />}
          {data && <QuotaCard storage={data} />}
          {data && <OperationProgress operation={data.operation} />}
          {data && <ArtifactRepoTable storage={data} config={config.data} offline={offline} />}
          {data && <RecentDeletions deletions={data.deletions} />}
        </TabsContent>
        <TabsContent value="self-hosted" className="flex flex-col gap-4">
          {selfHosted.isError && <ErrorLine onRetry={() => void selfHosted.refetch()}>{errorText(selfHosted.error)}</ErrorLine>}
          {!store && !selfHosted.isError && <Spinner label="Loading" />}
          {store && !store.configured && <ArtifactStoreEmpty />}
          {store?.configured && (
            <>
              {store.error && <ErrorLine>{`Could not list the bucket: ${store.error}`}</ErrorLine>}
              <ArtifactStoreUsage store={store} />
              <OperationProgress operation={store.operation} />
              <ArtifactStoreTable store={store} config={config.data} offline={offline} />
              <RecentDeletions deletions={mergeDeletions(data?.deletions ?? [], store.deletions)} showStore />
            </>
          )}
        </TabsContent>
      </Tabs>
    </div>
  )
}
