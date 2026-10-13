import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Confirm } from "@/components/confirm-dialog"
import { repositories } from "@/lib/format"
import { RETENTION_WARNING } from "@/lib/quota"

export function RetentionAll({ count, offline, busy, ask }: { count: number; offline: boolean; busy: boolean; ask: (c: Confirm) => void }) {
  const queryClient = useQueryClient()
  const [days, setDays] = useState("")
  const retention = useMutation({
    mutationFn: (n: number) => api.setRetentionAll(n),
    onSuccess: () => {
      toast.success("Retention change started")
      setDays("")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.githubStorage }),
  })
  const n = /^\d+$/.test(days.trim()) ? Number(days.trim()) : Number.NaN
  const ok = n >= 1 && n <= 400
  return (
    <div className="flex flex-col gap-1">
      <div className="flex flex-wrap items-center gap-2">
        <Label htmlFor="retention_all">Retention for all</Label>
        <Input id="retention_all" type="number" min={1} max={400} suffix="days" groupClassName="w-36" className="min-w-0" value={days} disabled={offline} onChange={(e) => setDays(e.target.value)} />
        <Button
          variant="outline"
          disabled={offline || busy || !ok || count === 0 || retention.isPending}
          onClick={() =>
            ask({
              title: `Set retention to ${n} days on ${repositories(count)}?`,
              body: `${RETENTION_WARNING} A repository whose maximum is lower is skipped.`,
              action: "Set retention",
              run: () => retention.mutate(n),
            })
          }
        >
          Set retention
        </Button>
      </div>
      {days !== "" && !ok && <p className="text-xs text-destructive">Enter a whole number of days from 1 to 400</p>}
    </div>
  )
}
