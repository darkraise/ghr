import { useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { PruneScope } from "@/api/types"
import type { Confirm } from "@/components/confirm-dialog"
import type { PruneAsk } from "./storage"

// A prune shows in /status (maintenance) as well as /storage, so both are
// read again once it is sent.
export function usePrune() {
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const prune = useMutation({
    mutationFn: (scope: PruneScope) => api.prune(scope),
    onSuccess: () => {
      toast.success("Prune started")
    },
    onSettled: () =>
      Promise.all([queryClient.invalidateQueries({ queryKey: keys.storage }), queryClient.invalidateQueries({ queryKey: keys.status })]),
  })
  return {
    confirm,
    close: () => setConfirm(null),
    ask: (scope: PruneScope, c: PruneAsk) =>
      setConfirm({ title: c.title, body: c.body, action: c.action, destructive: c.destructive, run: () => prune.mutate(scope) }),
  }
}
