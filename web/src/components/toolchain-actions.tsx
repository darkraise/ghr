import { Button } from "darkraise-ui/components/button"
import { useState } from "react"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { InstallDialog } from "@/components/install-dialog"
import { usePopularSet } from "@/lib/use-popular-set"

export function ToolchainActions({ offline }: { offline: boolean }) {
  const popular = usePopularSet()
  const [installing, setInstalling] = useState(false)
  return (
    <div className="flex flex-wrap gap-2">
      <Button disabled={offline} onClick={() => setInstalling(true)}>
        Install
      </Button>
      <Button variant="outline" disabled={offline} onClick={popular.ask}>
        Install popular set
      </Button>
      <ConfirmDialog confirm={popular.confirm} onClose={popular.close} />
      <InstallDialog open={installing} onClose={() => setInstalling(false)} />
    </div>
  )
}
