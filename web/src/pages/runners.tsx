import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useState } from "react"
import { useLogTail, useStatus } from "@/api/hooks"
import { LogView } from "@/components/log-view"
import { RunnersTable } from "@/components/runners-table"

export function RunnersPage() {
  const status = useStatus()
  const [picked, setPicked] = useState<string | null>(null)
  const [follow, setFollow] = useState(true)
  const instances = status.data?.instances ?? []
  const first = instances[0]?.id
  // Until the owner picks one, the first runner is the selection. A picked
  // runner that ends stays picked, so its last log stays readable.
  if (picked === null && first !== undefined) setPicked(first)
  const live = picked !== null && instances.some((i) => i.id === picked)
  const log = useLogTail(picked ?? "", live)
  const title = picked === null ? "Log preview — no runner selected" : `Log preview — ${picked} (${live ? "following" : "ended"})`
  return (
    <>
      <PageHeader title="Runners" />
      <Card>
        <CardContent className="max-h-[50vh] overflow-auto p-4">
          {status.data ? (
            <RunnersTable
              status={status.data}
              actions
              selected={live ? picked : null}
              onSelect={(id) => {
                setPicked(id)
                setFollow(true)
              }}
            />
          ) : (
            <Spinner label="waiting for the daemon…" />
          )}
        </CardContent>
      </Card>
      <Card className="mt-4">
        <CardHeader>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        <CardContent>
          {picked !== null && <LogView text={log.data?.text ?? ""} follow={follow} onFollowChange={setFollow} className="h-[17.5rem]" />}
        </CardContent>
      </Card>
    </>
  )
}
