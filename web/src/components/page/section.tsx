import { useId, type ReactNode } from "react"

export function Section({
  id,
  title,
  aside,
  children,
  className = "",
}: {
  id?: string
  title: string
  aside?: ReactNode
  children: ReactNode
  className?: string
}) {
  const headingId = useId()
  return (
    <section id={id} aria-labelledby={headingId} className={`min-w-0 rounded-[10px] border border-border bg-card p-4 ${className}`}>
      <div className="mb-3 flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h2 id={headingId} className="text-base font-semibold">
          {title}
        </h2>
        {aside}
      </div>
      {children}
    </section>
  )
}
