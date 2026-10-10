import type { ReactNode } from "react"

export function SideGroup({ children }: { children: ReactNode }) {
  return <p className="mt-3 border-t border-border px-3 pt-3 pb-1 text-xs font-semibold text-muted-foreground">{children}</p>
}

// A 240px list of links beside the content; below 800px the list stacks above it.
export function SideLayout({ label, nav, children }: { label: string; nav: ReactNode; children: ReactNode }) {
  return (
    <div className="grid gap-6 min-[800px]:grid-cols-[15rem_minmax(0,1fr)] min-[800px]:items-start">
      <nav aria-label={label} className="flex flex-col gap-0.5 min-[800px]:sticky min-[800px]:top-4">
        {nav}
      </nav>
      <div className="flex min-w-0 flex-col gap-4">{children}</div>
    </div>
  )
}
