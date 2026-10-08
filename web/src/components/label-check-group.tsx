import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { TriangleAlert } from "lucide-react"
import { useState, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys, useLabelCheck, useStatus } from "@/api/hooks"
import { ErrorLine } from "@/components/page/error-line"
import { FieldGroup } from "@/components/page/field"
import { StateText } from "@/components/page/state-text"
import { ago, plural } from "@/lib/format"
import { classify, OS_ARCH } from "@/lib/labels"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

export function LabelCheckGroup({
  name,
  effective,
  degraded,
  degradedReason,
  disabled,
  onAddLabel,
}: {
  name: string
  effective: string[]
  degraded: boolean
  degradedReason?: string
  disabled: boolean
  onAddLabel: (label: string) => void
}) {
  const now = useNow()
  const queryClient = useQueryClient()
  // Wait for the first status: until it arrives, a degraded daemon looks healthy.
  const statusRead = useStatus().data !== undefined
  const check = useLabelCheck(name, statusRead && !degraded)
  const [starting, setStarting] = useState(false)
  const lc = degraded ? undefined : check.data

  async function start() {
    setStarting(true)
    try {
      await api.startLabelCheck(name)
      await queryClient.invalidateQueries({ queryKey: keys.labelCheck(name) })
    } catch (err) {
      toast.error(`Label check: ${errorText(err)}`)
    } finally {
      setStarting(false)
    }
  }

  let state: ReactNode = "Not checked yet"
  if (lc?.state === "checking") state = <Spinner label="Checking" />
  else if (lc?.state === "done" && lc.checked_at) state = `Checked ${ago(now - Date.parse(lc.checked_at))}${lc.partial ? ", partial" : ""}`

  // A label missing from several groups gets one Add button, on its first group.
  const offered = new Set<string>()
  const groups = (lc?.groups ?? []).map((g, i) => {
    const { kind, missing } = classify(g.labels, effective)
    const adds = missing.filter((l) => !OS_ARCH.has(l) && !offered.has(l))
    for (const l of adds) offered.add(l)
    return (
      <div key={i} className="flex flex-col gap-1.5 py-3 text-sm">
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <StateText state={kind} />
          {g.labels.length > 0 ? <span className="font-mono">{g.labels.join(", ")}</span> : <span>No labels (runner group)</span>}
          <span>{g.more > 0 ? `${g.jobs.join(", ")} +${g.more} more` : g.jobs.join(", ")}</span>
          <span className="text-muted-foreground">{`${plural(g.count, "job")}, last seen ${ago(now - Date.parse(g.last_seen))}`}</span>
        </div>
        {kind === "unmatched" && (
          <>
            <p>{`Missing ${missing.join(", ")}`}</p>
            {adds.length > 0 && (
              <div className="flex flex-wrap gap-2">
                {adds.map((l) => (
                  <Button key={l} size="sm" variant="outline" disabled={disabled} onClick={() => onAddLabel(l)}>
                    {`Add label ${l}`}
                  </Button>
                ))}
              </div>
            )}
            {missing.some((l) => OS_ARCH.has(l)) && (
              <p className="flex items-center gap-1.5 text-muted-foreground">
                <TriangleAlert size={15} aria-hidden="true" className="shrink-0 text-warning" />
                Needs a different OS or architecture
              </p>
            )}
          </>
        )}
      </div>
    )
  })

  return (
    <FieldGroup id="workflow-labels" title="Workflow labels">
      <div className="flex flex-wrap items-center gap-3 py-3 text-sm">
        <span>{state}</span>
        <Button size="sm" variant="outline" disabled={disabled || degraded || starting || lc?.state === "checking"} onClick={() => void start()}>
          Check now
        </Button>
      </div>
      {degraded && (
        <div className="py-3">
          <ErrorLine>{`GitHub is rejecting the token: ${degradedReason ?? ""}`}</ErrorLine>
        </div>
      )}
      {lc?.error && (
        <div className="py-3">
          <ErrorLine>{lc.error}</ErrorLine>
        </div>
      )}
      {check.isError && !degraded && (
        <div className="py-3">
          <ErrorLine>{errorText(check.error)}</ErrorLine>
        </div>
      )}
      {groups}
      {offered.size > 0 && (
        <p className="py-3 text-xs text-muted-foreground">Adding a label changes which jobs ghr accepts. It does not install anything on the runner.</p>
      )}
    </FieldGroup>
  )
}
