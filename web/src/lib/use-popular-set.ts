import { useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Confirm } from "@/components/confirm-dialog"

// The daemon's popular preset (internal/toolchain/set.go), listed in the
// same words the confirmation shows before queueing it.
const POPULAR = "node 22, node 24, dotnet 8.0, dotnet 10.0, python 3.13, python 3.14, go latest, java 21, java 25. Versions already installed are skipped."

export function usePopularSet() {
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const popular = useMutation({
    mutationFn: () => api.installPreset("popular"),
    onSuccess: () => {
      toast.success("Queued: popular set")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.storage }),
  })
  return {
    confirm,
    close: () => setConfirm(null),
    ask: () => setConfirm({ title: "Install the popular set?", body: POPULAR, action: "Install popular set", run: () => popular.mutate() }),
  }
}
