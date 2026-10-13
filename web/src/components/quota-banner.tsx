import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { TriangleAlert } from "lucide-react"
import { useId, useState } from "react"
import type { StorageAlert } from "@/api/types"
import { bannerText, canDismiss, dismiss, isDismissed } from "@/lib/quota"

const BANNER = "flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border bg-card px-4 py-2.5 text-sm text-card-foreground"

export function QuotaBanner({ alert }: { alert: StorageAlert }) {
  const titleId = useId()
  const key = `${alert.month}:${alert.level}`
  // Kept in memory too, so Dismiss works for this visit without local storage.
  const [hidden, setHidden] = useState<string | null>(null)
  if (alert.level === "" || hidden === key || isDismissed(alert)) return null
  const early = canDismiss(alert.level)
  return (
    <section aria-labelledby={titleId} className={`${BANNER} ${early ? "border-warning" : "border-destructive"}`}>
      <h2 id={titleId} className={`flex items-center gap-1.5 font-semibold ${early ? "text-warning" : "text-destructive"}`}>
        <TriangleAlert size={15} aria-hidden="true" />
        {bannerText(alert)}
      </h2>
      <Link to="/artifacts" className="text-primary hover:underline">
        Go to Artifacts
      </Link>
      {early && (
        <Button
          size="sm"
          variant="ghost"
          className="ml-auto"
          onClick={() => {
            dismiss(alert)
            setHidden(key)
          }}
        >
          Dismiss
        </Button>
      )}
    </section>
  )
}
