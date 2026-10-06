import { Card, CardContent } from "darkraise-ui/components/card"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useStatus } from "@/api/hooks"
import { RunnersTable } from "@/components/runners-table"

export function RunnersPage() {
  const status = useStatus()
  return (
    <>
      <PageHeader title="Runners" />
      <Card>
        <CardContent className="p-4">
          {status.data ? <RunnersTable status={status.data} actions /> : <Spinner label="waiting for the daemon…" />}
        </CardContent>
      </Card>
    </>
  )
}
