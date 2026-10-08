import type { ReactNode } from "react"
import { useMediaQuery, WIDE } from "@/lib/use-media-query"

// One tree for both widths, with the panel in the second slot: content passed
// as both panel and narrow keeps its state when the width crosses 1024px.
export function SplitView({ list, panel, narrow }: { list: ReactNode; panel: ReactNode | null; narrow: ReactNode }) {
  const wide = useMediaQuery(WIDE)
  const split = wide && panel !== null
  return (
    <div className={split ? "grid grid-cols-[22rem_minmax(0,1fr)] items-start gap-4" : "min-w-0"}>
      {wide && <div className="min-w-0">{list}</div>}
      <div className="min-w-0">{wide ? panel : narrow}</div>
    </div>
  )
}
