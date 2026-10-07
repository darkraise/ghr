import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { RepoStatus } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"

// Shared by the Dashboard and the Repositories page so the two cannot drift.
export function RepoActionButtons({ repo, offline }: { repo: RepoStatus; offline: boolean }) {
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
      toast.success(`${resume ? "resumed" : "paused"} ${repo.name}`)
    },
    onSettled: refresh,
  })
  const remove = useMutation({
    mutationFn: () => api.removeRepo(repo.name),
    onSuccess: () => {
      toast.success(`removing ${repo.name}`)
    },
    onSettled: refresh,
  })
  const locked = offline || Boolean(repo.removing)
  return (
    <>
      <Button
        size="sm"
        variant="secondary"
        aria-label={`${repo.paused ? "Resume" : "Pause"} ${repo.name}`}
        disabled={locked}
        onClick={() => pause.mutate(repo.paused)}
      >
        {repo.paused ? "Resume" : "Pause"}
      </Button>
      <Button
        size="sm"
        variant="destructive"
        aria-label={`Remove ${repo.name}`}
        disabled={locked}
        onClick={() =>
          setConfirm({
            title: `Remove repo ${repo.name}? Its running jobs finish first.`,
            action: "Remove",
            destructive: true,
            run: () => remove.mutate(),
          })
        }
      >
        Remove
      </Button>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}
