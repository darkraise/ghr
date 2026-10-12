import { Button } from "darkraise-ui/components/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "darkraise-ui/components/dropdown-menu"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { Ellipsis } from "lucide-react"
import type { RepoStatus } from "@/api/types"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { useRepoActions } from "@/lib/use-repo-actions"

// Changing a cap is rare, so the max field lives on the repository page and
// the row keeps one menu.
export function RepoRowMenu({ repo, offline, onEdit }: { repo: RepoStatus; offline: boolean; onEdit: (name: string) => void }) {
  const actions = useRepoActions(repo, offline)
  return (
    <>
      <DropdownMenu>
        <Tooltip>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              <Button size="icon" variant="ghost" aria-label={`More actions for ${repo.name}`}>
                <Ellipsis size={15} aria-hidden="true" />
              </Button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent>More actions</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="end">
          <DropdownMenuItem disabled={actions.toggleLocked} onSelect={actions.togglePause}>
            {actions.toggleLabel}
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => onEdit(repo.name)}>Edit</DropdownMenuItem>
          <DropdownMenuItem disabled={actions.locked} onSelect={actions.askRemove} className="text-destructive">
            Remove
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <ConfirmDialog confirm={actions.confirm} onClose={actions.closeConfirm} />
    </>
  )
}
