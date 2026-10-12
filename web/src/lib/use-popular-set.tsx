import { useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Confirm } from "@/components/confirm-dialog"
import { POPULAR, toolName } from "@/lib/toolchains"

const LINES = [...new Set(POPULAR.map((p) => p.tool))].map((tool) => ({
  tool,
  specs: POPULAR.filter((p) => p.tool === tool).map((p) => p.spec),
}))

const LIST = (
  <ul aria-label="Popular set" className="flex flex-col divide-y divide-border rounded-md border border-border text-sm">
    {LINES.map((l) => (
      <li key={l.tool} className="flex items-center justify-between gap-4 px-3 py-1.5">
        <span>{toolName(l.tool)}</span>
        <span className="font-mono text-muted-foreground">{l.specs.join(", ")}</span>
      </li>
    ))}
  </ul>
)

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
    ask: () =>
      setConfirm({
        title: "Install the popular set?",
        body: "Each line installs its newest release, unless that release is already installed.",
        details: LIST,
        action: "Install popular set",
        run: () => popular.mutate(),
      }),
  }
}
