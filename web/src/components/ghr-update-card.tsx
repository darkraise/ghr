import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Spinner } from "darkraise-ui/components/spinner"
import { Download } from "lucide-react"
import { useId, useState } from "react"
import type { GhrUpdate } from "@/api/types"

const BANNER = "flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border bg-card px-4 py-2.5 text-sm text-card-foreground"

const STEPS: Record<string, string> = {
  download: "downloading",
  verify: "verifying the checksum and the archive",
  smoke: "testing the new binary",
  waiting: "waiting for running work to finish",
  swap: "installing the files",
}

// The release the owner dismissed; a newer one shows the banner again.
const DISMISSED = "ghr.dismissedUpdate"

// Storage can be refused (a private window, a blocked site); the banner
// then stays dismissed for this page load only.
function readDismissed(): string | null {
  try {
    return localStorage.getItem(DISMISSED)
  } catch {
    return null
  }
}

function writeDismissed(tag: string) {
  try {
    localStorage.setItem(DISMISSED, tag)
  } catch {
    // Kept in the component's state instead.
  }
}

function message(u: GhrUpdate): string | null {
  if (u.installing && u.step !== "restart") return `Installing ghr ${u.latest ?? ""}: ${STEPS[u.step ?? ""] ?? "starting"}`
  if (u.restart_pending && u.last_outcome === "failed") return `ghr ${u.restart_pending} is installed, but the restart failed.`
  if (u.restart_pending) return `Restarting into ${u.restart_pending}`
  return null
}

export function GhrUpdateCard({ update }: { update: GhrUpdate | undefined }) {
  const titleId = useId()
  const [dismissed, setDismissed] = useState(readDismissed)
  if (!update) return null
  const progress = message(update)
  const offer = progress === null && update.newer === true && update.latest !== undefined && update.latest !== dismissed
  if (progress === null && !offer) return null
  const dismiss = () => {
    const tag = update.latest ?? ""
    writeDismissed(tag)
    setDismissed(tag)
  }
  return (
    <section aria-labelledby={titleId} className={`${BANNER} border-border`}>
      <h2 id={titleId} className="flex items-center gap-1.5 font-semibold">
        <Download size={15} aria-hidden="true" />
        ghr update
      </h2>
      {progress !== null ? (
        <p className="flex items-center gap-2">
          {update.last_outcome !== "failed" && <Spinner />}
          <span>{progress}</span>
        </p>
      ) : (
        <p>{`ghr ${update.latest ?? ""} is available. This host runs ${update.installed}.`}</p>
      )}
      <Link to="/settings/$section" params={{ section: "updates" }} className="underline">
        Open Updates
      </Link>
      {offer && (
        <Button size="sm" variant="ghost" onClick={dismiss}>
          Dismiss
        </Button>
      )}
    </section>
  )
}
