import { Spinner } from "darkraise-ui/components/spinner"
import { CircleX, Info, TriangleAlert, type LucideIcon } from "lucide-react"
import type { Annotation, RunAnnotations } from "@/api/types"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import type { JobLinkFn } from "@/components/run-frame"
import { plural } from "@/lib/format"
import { plainText } from "@/lib/ansi"
import { annotationCounts } from "@/lib/run-details"
import { errorText } from "@/query"

const LEVELS: Record<string, { label: string; Icon: LucideIcon; className: string } | undefined> = {
  error: { label: "Error", Icon: CircleX, className: "text-destructive" },
  warning: { label: "Warning", Icon: TriangleAlert, className: "text-warning" },
  notice: { label: "Notice", Icon: Info, className: "text-muted-foreground" },
}

function LevelIcon({ level }: { level: string }) {
  const { label, Icon, className } = LEVELS[level] ?? { label: "Notice", Icon: Info, className: "text-muted-foreground" }
  return <Icon role="img" aria-label={label} size={15} className={`mt-0.5 shrink-0 ${className}`} />
}

// Each line shows only when its count is above zero.
function statusLines(data: RunAnnotations): string[] {
  const out: string[] = []
  if (data.unread > 0) out.push(`Reading logs for ${plural(data.unread, "job")}`)
  if (data.expired_logs > 0) {
    out.push(data.expired_logs === 1 ? "The log for 1 job has expired" : `Logs for ${data.expired_logs} jobs have expired`)
  }
  if (data.too_large > 0) {
    out.push(data.too_large === 1 ? "1 job log is too large to read here" : `${data.too_large} job logs are too large to read here`)
  }
  if (data.failed > 0) out.push(data.failed === 1 ? "1 job log could not be read" : `${data.failed} job logs could not be read`)
  return out
}

// Rows are sorted by level first, so a job's rows need not be adjacent; its
// "more" line goes under the last of them.
function isLastOfJob(items: Annotation[], i: number): boolean {
  const id = items[i]?.job_id
  return !items.slice(i + 1).some((a) => a.job_id === id)
}

function Counts({ items }: { items: Annotation[] }) {
  const c = annotationCounts(items)
  const parts = [
    c.error > 0 && plural(c.error, "error"),
    c.warning > 0 && plural(c.warning, "warning"),
    c.notice > 0 && plural(c.notice, "notice"),
  ].filter((p): p is string => typeof p === "string")
  if (parts.length === 0) return null
  return (
    <span className="flex flex-wrap gap-x-3 text-sm text-muted-foreground">
      {parts.map((p) => (
        <span key={p} className="font-mono">
          {p}
        </span>
      ))}
    </span>
  )
}

export function AnnotationsSection({
  data,
  error,
  onRetry,
  jobLink,
}: {
  data: RunAnnotations | undefined
  error: unknown
  onRetry: () => void
  jobLink: JobLinkFn
}) {
  if (!data) {
    return (
      <Section title="Annotations">
        {error ? <ErrorLine onRetry={onRetry}>{errorText(error)}</ErrorLine> : <Spinner label="Loading" />}
      </Section>
    )
  }
  const lines = statusLines(data)
  return (
    <Section title="Annotations" aside={<Counts items={data.items} />}>
      {error ? <ErrorLine onRetry={onRetry}>{errorText(error)}</ErrorLine> : null}
      {data.items.length === 0 && lines.length === 0 ? (
        <p className="text-sm text-muted-foreground">No errors, warnings or notices in the finished jobs&apos; logs.</p>
      ) : (
        <>
          {data.items.length > 0 && (
            <ul className="flex flex-col">
              {data.items.map((a, i) => {
                const more = isLastOfJob(data.items, i) ? (data.omitted[String(a.job_id)] ?? 0) : 0
                return (
                  <li key={`${a.job_id}-${a.step}-${a.line}-${a.level}`} className="border-b border-border py-2 last:border-b-0">
                    <div className="flex gap-2">
                      <LevelIcon level={a.level} />
                      <div className="flex min-w-0 flex-col gap-1">
                        <p className="font-mono text-sm break-words whitespace-pre-wrap">{plainText(a.message)}</p>
                        <p className="flex flex-wrap gap-x-3 text-xs text-muted-foreground">
                          <span>{`${a.job_name} / ${a.step_name}`}</span>
                          {jobLink(a.job_id, `Log line ${a.line}`, {
                            step: a.step,
                            line: a.line,
                            className: "text-primary hover:underline",
                            label: `Log line ${a.line}, ${a.job_name} / ${a.step_name}`,
                          })}
                        </p>
                        {more > 0 && <p className="text-xs text-muted-foreground">{`${more} more in the log`}</p>}
                      </div>
                    </div>
                  </li>
                )
              })}
            </ul>
          )}
          {lines.map((l) => (
            <p key={l} className="mt-2 text-sm text-muted-foreground">
              {l}
            </p>
          ))}
          <p className="mt-3 text-xs text-muted-foreground">Taken from the job logs, so file and line come only from the message text.</p>
        </>
      )}
    </Section>
  )
}
