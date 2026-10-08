import { Button } from "darkraise-ui/components/button"
import type { RepoStatus } from "@/api/types"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { useRepoActions } from "@/lib/use-repo-actions"

export function RepoActionButtons({ repo, offline }: { repo: RepoStatus; offline: boolean }) {
  const actions = useRepoActions(repo, offline)
  return (
    <>
      <Button
        size="sm"
        variant="secondary"
        aria-label={`${repo.paused ? "Resume" : "Pause"} ${repo.name}`}
        disabled={actions.locked}
        onClick={actions.togglePause}
      >
        {repo.paused ? "Resume" : "Pause"}
      </Button>
      <Button size="sm" variant="destructive" aria-label={`Remove ${repo.name}`} disabled={actions.locked} onClick={actions.askRemove}>
        Remove
      </Button>
      <ConfirmDialog confirm={actions.confirm} onClose={actions.closeConfirm} />
    </>
  )
}
