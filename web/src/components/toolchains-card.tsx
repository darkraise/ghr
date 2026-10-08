import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { toast } from "darkraise-ui/components/sonner"
import { Table, TableBody, TableCell, TableRow } from "darkraise-ui/components/table"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Status, Storage, Toolchain } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { RefusedHint } from "@/components/refused-hint"
import { ToolchainActions } from "@/components/toolchain-actions"
import { dateTime, humanBytes } from "@/lib/format"
import { lastDotnetMajor, queueText } from "@/lib/toolchains"

export function ToolchainsCard({ storage, status, offline }: { storage: Storage; status: Status | undefined; offline: boolean }) {
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const settled = () => queryClient.invalidateQueries({ queryKey: keys.storage })
  const remove = useMutation({
    mutationFn: (t: Toolchain) => api.removeToolchain(t.tool, t.version),
    onSuccess: (_data, t) => {
      toast.success(`queued: remove ${t.tool} ${t.version}`)
    },
    onSettled: settled,
  })
  const tools = storage.toolchains ?? []
  const others = storage.other_tool_cache ?? []
  const queue = queueText(storage.operations)

  function askRemove(t: Toolchain) {
    const major = lastDotnetMajor(t, tools)
    let title = `Remove ${t.tool} ${t.version} (${humanBytes(t.bytes)})?`
    if (major) title += ` It is the last .NET ${major} SDK, so this also removes the ${major}.0 runtimes and packs.`
    setConfirm({ title, action: "Remove", destructive: true, run: () => remove.mutate(t) })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Toolchains</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {tools.length === 0 && others.length === 0 ? (
          <p className="text-sm text-muted-foreground">the tool cache is empty</p>
        ) : (
          <Table>
            <TableBody>
              {tools.map((t) => (
                <TableRow key={`${t.tool}/${t.version}/${t.arch}`}>
                  <TableCell>{`${t.tool} ${t.version}`}</TableCell>
                  <TableCell>{humanBytes(t.bytes)}</TableCell>
                  <TableCell className="text-muted-foreground">{`installed ${dateTime(t.installed_at).slice(0, 10)}`}</TableCell>
                  <TableCell className="text-right">
                    <Button
                      size="sm"
                      variant="destructive"
                      aria-label={`Remove ${t.tool} ${t.version}`}
                      disabled={offline}
                      onClick={() => askRemove(t)}
                    >
                      Remove
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
              {others.map((f) => (
                <TableRow key={`other/${f.name}`}>
                  <TableCell>{f.name}</TableCell>
                  <TableCell>{humanBytes(f.bytes)}</TableCell>
                  <TableCell colSpan={2} className="text-muted-foreground">
                    {"other: a job's own setup step"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        {queue && <p className="text-sm text-amber-600">{queue}</p>}
        <RefusedHint what="Remove" status={status} />
        <ToolchainActions offline={offline} />
      </CardContent>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Card>
  )
}
