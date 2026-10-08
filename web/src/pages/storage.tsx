import { Button } from "darkraise-ui/components/button"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useConfig, useStatus, useStorage } from "@/api/hooks"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { PruneSection } from "@/components/prune-section"
import { OperationsCard, PackageCachesCard } from "@/components/storage-cards"
import { DiskSection } from "@/components/storage-disk"
import { standardPrune, storageSummary } from "@/lib/storage"
import { useOperationToasts } from "@/lib/use-operation-toasts"
import { usePrune } from "@/lib/use-prune"
import { errorText } from "@/query"

// Below 1280px the sections stack in reading order. At 1280px and wider
// they form a 3:2 grid: Disk over Package caches, Prune over Recent
// operations.
const PLACE = {
  disk: "xl:col-start-1 xl:row-start-1",
  prune: "xl:col-start-2 xl:row-start-1",
  caches: "xl:col-start-1 xl:row-start-2",
  operations: "xl:col-start-2 xl:row-start-2",
}

export function StoragePage() {
  const status = useStatus()
  const config = useConfig()
  const storage = useStorage(status.data)
  useOperationToasts(storage.data)
  const prune = usePrune()
  const st = status.data
  const data = storage.data
  const offline = status.isError
  const highWater = config.data?.disk_high_water ?? 80
  const disabled = offline || (st?.maintenance.running ?? false)
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Storage"
        description={st ? storageSummary(st, highWater) : undefined}
        actions={
          <Button disabled={disabled} onClick={() => prune.ask("standard", standardPrune(config.data))}>
            Prune
          </Button>
        }
      />
      {storage.isError && <ErrorLine onRetry={() => void storage.refetch()}>{errorText(storage.error)}</ErrorLine>}
      {data && st ? (
        <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
          <DiskSection storage={data} status={st} highWater={highWater} className={PLACE.disk} />
          <PruneSection storage={data} status={st} config={config.data} disabled={disabled} className={PLACE.prune} />
          <div className={PLACE.caches}>
            <PackageCachesCard storage={data} status={st} offline={offline} />
          </div>
          <div className={PLACE.operations}>
            <OperationsCard storage={data} />
          </div>
        </div>
      ) : (
        !storage.isError && <Spinner label="Loading" />
      )}
      <ConfirmDialog confirm={prune.confirm} onClose={prune.close} />
    </div>
  )
}
