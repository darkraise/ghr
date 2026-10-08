import { useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { RepoStatus } from "@/api/types"
import type { Confirm } from "@/components/confirm-dialog"

// Every control that pauses or removes a repository goes through here, so the
// Dashboard's row menu and the Repositories page cannot drift.
export function useRepoActions(repo: RepoStatus, offline: boolean) {
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const refresh = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: keys.status }),
      queryClient.invalidateQueries({ queryKey: keys.config }),
    ])
  const pause = useMutation({
    mutationFn: (resume: boolean) => (resume ? api.resumeRepo(repo.name) : api.pauseRepo(repo.name)),
    onSuccess: (_data, resume) => {
      toast.success(`${resume ? "Resumed" : "Paused"} ${repo.name}`)
    },
    onSettled: refresh,
  })
  const remove = useMutation({
    mutationFn: () => api.removeRepo(repo.name),
    onSuccess: () => {
      toast.success(`Removing ${repo.name}`)
    },
    onSettled: refresh,
  })
  return {
    locked: offline || Boolean(repo.removing),
    togglePause: () => pause.mutate(repo.paused),
    askRemove: () =>
      setConfirm({
        title: `Remove ${repo.name}?`,
        body: "Its running jobs finish first.",
        action: "Remove",
        destructive: true,
        run: () => remove.mutate(),
      }),
    confirm,
    closeConfirm: () => setConfirm(null),
  }
}
