import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import type { ArtifactStore } from "@/api/types"
import { SETUP_GUIDE } from "@/components/artifact-store-setup"
import { Section } from "@/components/page/section"
import { StatCard } from "@/components/stat-card"
import { count, humanBytes, shortWhen } from "@/lib/format"
import { useNow } from "@/lib/use-now"

// The Dashboard's tile grid, with a rule below in place of its frame.
const TILES =
  "grid grid-cols-2 divide-border border-b border-border [&>*:nth-child(-n+2)]:border-b [&>*:nth-child(odd)]:border-r min-[641px]:grid-cols-4 min-[641px]:divide-x min-[641px]:[&>*:nth-child(-n+2)]:border-b-0"

function host(endpoint: string | undefined): string {
  if (!endpoint) return ""
  try {
    return new URL(endpoint).host
  } catch {
    return endpoint
  }
}

function tint(share: number): string {
  if (share >= 0.9) return "bg-destructive"
  if (share >= 0.7) return "bg-warning"
  return "bg-primary"
}

export function ArtifactStoreUsage({ store }: { store: ArtifactStore }) {
  const now = useNow()
  const capacity = store.capacity_bytes ?? 0
  const share = capacity > 0 ? store.used_bytes / capacity : 0
  const used = humanBytes(store.used_bytes)
  const on = store.repos.filter((r) => r.enabled).length
  const privateRepos = store.repos.filter((r) => r.private).length
  const aside = store.checked_at && <span className="ml-auto font-mono text-sm text-muted-foreground">{`Listed ${shortWhen(store.checked_at, now)}`}</span>
  return (
    <Section id="rustfs-usage" title="RustFS storage" aside={aside} flush>
      <div className={TILES}>
        <StatCard label="Used" value={used} unit={capacity > 0 ? `of ${humanBytes(capacity)}` : undefined} />
        <StatCard label="Artifacts" value={count(store.count)} />
        <StatCard label="Repositories using RustFS" value={on} unit={`of ${privateRepos} private`} />
        <StatCard label="Bucket" value={<span className="text-sm break-all">{`${host(store.endpoint)} / ${store.bucket ?? ""}`}</span>} />
      </div>
      <div className="flex flex-col gap-3 p-4">
        {capacity > 0 && (
          <div role="img" aria-label={`${used} of ${humanBytes(capacity)} used`} className="flex h-3 overflow-hidden rounded-[3px] bg-muted">
            <span className={tint(share)} style={{ width: `${Math.min(100, Math.round(share * 1000) / 10)}%` }} />
          </div>
        )}
        <p className="text-xs text-muted-foreground">
          {capacity > 0
            ? "Capacity is the size you set in Settings; ghr does not read the VPS disk."
            : "Set a capacity in Settings to see use against it; ghr does not read the VPS disk."}
        </p>
      </div>
    </Section>
  )
}

export function ArtifactStoreEmpty() {
  return (
    <section aria-labelledby="rustfs-empty-title" className="flex flex-col gap-3 rounded-md border border-border bg-card p-6">
      <h2 id="rustfs-empty-title" className="text-base font-semibold">
        Keep artifacts on your own S3 storage
      </h2>
      <p className="text-sm text-muted-foreground">
        Repositories you switch on upload their artifacts to an S3-compatible server such as RustFS instead of GitHub, so they stop using your GitHub
        storage allowance. Jobs on GitHub-hosted runners use it too. Everything else stays on GitHub.
      </p>
      <p className="text-xs text-muted-foreground">
        Artifacts are deleted after 7 days unless a run sets retention-days or you change the cleanup age. GitHub keeps them 90 days by default, so check
        workflows that read old artifacts.
      </p>
      <ol className="flex list-decimal flex-col gap-1 pl-5 text-sm">
        <li>Create a bucket and two keys on the server: an admin key for ghr and a job key for workflows.</li>
        <li>Enter the connection and both keys under Settings → Self-hosted storage.</li>
        <li>
          Switch workflows to <span className="font-mono">darkraise/ghr/actions/upload</span> and <span className="font-mono">/download</span>, then turn
          repositories on here; ghr writes each one's variable and secret.
        </li>
      </ol>
      <div className="flex flex-wrap gap-2">
        <Button asChild>
          <Link to="/settings/$section" params={{ section: "self-hosted-storage" }}>
            Set up
          </Link>
        </Button>
        <Button variant="outline" asChild>
          <a href={SETUP_GUIDE} target="_blank" rel="noreferrer">
            Read the setup guide
          </a>
        </Button>
      </div>
    </section>
  )
}
