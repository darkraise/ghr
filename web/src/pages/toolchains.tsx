import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useStatus, useStorage } from "@/api/hooks"
import { ErrorLine } from "@/components/page/error-line"
import { ToolchainActions } from "@/components/toolchain-actions"
import { InstalledSection } from "@/components/toolchain-sections"
import { useOperationToasts } from "@/lib/use-operation-toasts"
import { errorText } from "@/query"

export function ToolchainsPage() {
  const status = useStatus()
  const storage = useStorage(status.data)
  useOperationToasts(storage.data)
  const offline = status.isError
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title="Toolchains" actions={<ToolchainActions offline={offline} />} />
      {storage.isError && <ErrorLine onRetry={() => void storage.refetch()}>{errorText(storage.error)}</ErrorLine>}
      {storage.data ? (
        <InstalledSection storage={storage.data} status={status.data} offline={offline} />
      ) : (
        !storage.isError && <Spinner label="Loading" />
      )}
    </div>
  )
}
