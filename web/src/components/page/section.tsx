import { useId, type ReactNode } from "react"

export function Section({
  id,
  title,
  aside,
  children,
  className = "",
  flush = false,
}: {
  id?: string
  title: string
  aside?: ReactNode
  children: ReactNode
  className?: string
  flush?: boolean
}) {
  const headingId = useId()
  return (
    <section id={id} aria-labelledby={headingId} className={`min-w-0 overflow-hidden rounded-md border border-border bg-card ${className}`}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border bg-background px-4 py-2.5">
        <h2 id={headingId} className="text-sm font-semibold">
          {title}
        </h2>
        {aside}
      </div>
      <div className={flush ? "" : "p-4"}>{children}</div>
    </section>
  )
}
