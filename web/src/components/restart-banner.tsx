import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { TriangleAlert } from "lucide-react"
import { useId } from "react"
import type { RestartStatus, Status, WebListenError } from "@/api/types"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { useRestart } from "@/lib/use-restart"

const BANNER = "flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border bg-card px-4 py-2.5 text-sm text-card-foreground"

function namesText(names: string[]): string {
  return names.length > 1 ? `${names.slice(0, -1).join(", ")} and ${names.at(-1) ?? ""}` : (names[0] ?? "")
}

export function RestartControl({ restart, offline }: { restart: RestartStatus; offline: boolean }) {
  const { ask, confirm, close, busy, ready } = useRestart()
  const blocked = restart.blocked_by ?? []
  return (
    <>
      {!restart.available && <p>Restart ghr with systemctl restart ghr.</p>}
      {blocked.length > 0 && <p>{`Waits for: ${blocked.join(", ")}`}</p>}
      {restart.available && (
        <>
          <Button size="sm" disabled={!ready || blocked.length > 0 || offline || busy} onClick={ask}>
            Restart ghr
          </Button>
          <ConfirmDialog confirm={confirm} onClose={close} />
        </>
      )}
    </>
  )
}

export function RestartBanner({ status, offline }: { status: Status; offline: boolean }) {
  const titleId = useId()
  const pending = status.restart.pending ?? []
  if (pending.length === 0) return null
  return (
    <section aria-labelledby={titleId} className={`${BANNER} border-warning`}>
      <h2 id={titleId} className="flex items-center gap-1.5 font-semibold text-warning">
        <TriangleAlert size={15} aria-hidden="true" />
        {`Restart needed: ${namesText(pending)} changed.`}
      </h2>
      <RestartControl restart={status.restart} offline={offline} />
    </section>
  )
}

export function ListenErrorBanner({ error }: { error: WebListenError }) {
  const titleId = useId()
  const serving = error.serving_on ? ` Serving on ${error.serving_on}.` : ""
  return (
    <section aria-labelledby={titleId} className={`${BANNER} border-destructive`}>
      <h2 id={titleId} className="flex items-center gap-1.5 font-semibold text-destructive">
        <TriangleAlert size={15} aria-hidden="true" />
        {`ghr could not listen on ${error.address}: ${error.reason.replace(/\.$/, "")}.${serving}`}
      </h2>
      <Link to="/settings" hash="web-access" className="text-primary hover:underline">
        Go to Web access
      </Link>
    </section>
  )
}
