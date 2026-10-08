import { PageHeader } from "darkraise-ui/layout"
import { useStatus, useStorage } from "@/api/hooks"
import { ToolchainsCard } from "@/components/toolchains-card"
import { useOperationToasts } from "@/lib/use-operation-toasts"
import { errorText } from "@/query"

export function ToolchainsPage() {
  const status = useStatus()
  const storage = useStorage(status.data)
  useOperationToasts(storage.data)
  return (
    <>
      <PageHeader title="Toolchains" />
      {storage.isError && <p className="mb-4 text-sm text-destructive">✖ {errorText(storage.error)}</p>}
      {storage.data ? (
        <ToolchainsCard storage={storage.data} status={status.data} offline={status.isError} />
      ) : (
        !storage.isError && <p className="text-sm text-muted-foreground">loading…</p>
      )}
    </>
  )
}
