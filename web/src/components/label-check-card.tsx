import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useLabelCheck, useStatus } from "@/api/hooks"
import { StateBadge } from "@/components/state-badge"
import { ago } from "@/lib/format"
import { classify, OS_ARCH } from "@/lib/labels"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

export function LabelCheckCard({
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
      toast.error(`label check: ${errorText(err)}`)
    } finally {
      setStarting(false)
    }
  }

  let header = "Not checked yet"
  if (lc?.state === "checking") header = "Checking…"
  else if (lc?.state === "done" && lc.checked_at) header = `Checked ${ago(now - Date.parse(lc.checked_at))}${lc.partial ? " · partial" : ""}`

  const offered = new Set<string>()
  return (
    <Card>
      <CardHeader>
        <CardTitle>Workflow labels</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        <div className="flex items-center gap-3">
          <span>{header}</span>
          <Button size="sm" variant="outline" disabled={disabled || degraded || starting || lc?.state === "checking"} onClick={() => void start()}>
            Check now
          </Button>
        </div>
        {degraded && <p className="text-destructive">GitHub is rejecting the token: {degradedReason}</p>}
        {lc?.error && <p className="text-destructive">✖ {lc.error}</p>}
        {check.isError && !degraded && <p className="text-destructive">✖ {errorText(check.error)}</p>}
        <ul className="flex flex-col gap-3">
          {(lc?.groups ?? []).map((g, i) => {
            const { kind, missing } = classify(g.labels, effective)
            const adds = missing.filter((l) => !OS_ARCH.has(l) && !offered.has(l))
            for (const l of adds) offered.add(l)
            return (
              <li key={i} className="flex flex-col gap-1">
                <div className="flex flex-wrap items-center gap-2">
                  <StateBadge state={kind} />
                  <span>{g.labels.length > 0 ? g.labels.join(", ") : "no labels (runner group)"}</span>
                  <span>{g.more > 0 ? `${g.jobs.join(", ")} +${g.more} more` : g.jobs.join(", ")}</span>
                  <span className="text-muted-foreground">
                    · {g.count} jobs · {ago(now - Date.parse(g.last_seen))}
                  </span>
                </div>
                {kind === "unmatched" && (
                  <div className="flex flex-col gap-1 pl-4">
                    <p>missing {missing.join(", ")}</p>
                    {adds.length > 0 && (
                      <div className="flex flex-wrap gap-2">
                        {adds.map((l) => (
                          <Button key={l} size="sm" variant="secondary" disabled={disabled} onClick={() => onAddLabel(l)}>
                            + add {l}
                          </Button>
                        ))}
                      </div>
                    )}
                    {missing.some((l) => OS_ARCH.has(l)) && <p className="text-amber-600">needs a different OS or architecture</p>}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
        {offered.size > 0 && (
          <p className="text-xs text-muted-foreground">
            Adding a label changes which jobs ghr accepts; it does not install anything on the runner.
          </p>
        )}
      </CardContent>
    </Card>
  )
}
