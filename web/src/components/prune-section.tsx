import { Button } from "darkraise-ui/components/button"
import type { Config, Status, Storage } from "@/api/types"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { Section } from "@/components/page/section"
import { RefusedHint } from "@/components/refused-hint"
import { pruneRows } from "@/lib/storage"
import { usePrune } from "@/lib/use-prune"

export function PruneSection({
  storage,
  status,
  config,
  disabled,
  className = "",
}: {
  storage: Storage
  status: Status | undefined
  config: Config | undefined
  disabled: boolean
  className?: string
}) {
  const prune = usePrune()
  return (
    <Section title="Prune" className={className}>
      <div className="flex flex-col divide-y divide-border">
        {pruneRows(storage, config).map((row) => (
          <div key={row.scope} className="flex flex-col gap-2 py-3 first:pt-0 last:pb-0">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <span className="text-sm">{row.text}</span>
              <Button size="sm" variant={row.destructive ? "destructive" : "outline"} disabled={disabled} onClick={() => prune.ask(row.scope, row)}>
                {row.action}
              </Button>
            </div>
            {row.scope === "unused-volumes" && <RefusedHint what="Unused volumes" status={status} />}
          </div>
        ))}
      </div>
      <ConfirmDialog confirm={prune.confirm} onClose={prune.close} />
    </Section>
  )
}
