import { Button } from "darkraise-ui/components/button"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useConfig, useStatus, useStorage } from "@/api/hooks"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { DiskSection } from "@/components/storage-disk"
import { ActivitySection, PackageCachesSection } from "@/components/storage-tables"
import { standardPrune, storageSummary } from "@/lib/storage"
import { useOperationToasts } from "@/lib/use-operation-toasts"
import { usePrune } from "@/lib/use-prune"
import { errorText } from "@/query"

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
        <>
          <DiskSection storage={data} status={st} config={config.data} highWater={highWater} disabled={disabled} />
          <PackageCachesSection storage={data} status={st} offline={offline} />
          <ActivitySection storage={data} status={st} />
        </>
      ) : (
        !storage.isError && <Spinner label="Loading" />
      )}
      <ConfirmDialog confirm={prune.confirm} onClose={prune.close} />
    </div>
  )
}
