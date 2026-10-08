import { Label } from "darkraise-ui/components/label"
import { CircleAlert } from "lucide-react"
import type { ReactNode } from "react"

// The heading takes focus when the section index jumps here, so it carries
// tabIndex -1 and an id the index can find.
export function FieldGroup({ id, title, note, children }: { id: string; title: string; note?: string; children: ReactNode }) {
  return (
    <section id={id} aria-labelledby={`${id}-title`} className="scroll-mt-4">
      <h2 id={`${id}-title`} tabIndex={-1} className="text-base font-semibold">
        {title}
      </h2>
      {note && <p className="mt-1 text-sm text-muted-foreground">{note}</p>}
      <div className="mt-2 divide-y divide-border border-y border-border">{children}</div>
    </section>
  )
}

export function Field({
  label,
  htmlFor,
  help,
  error,
  changed,
  children,
}: {
  label: string
  htmlFor?: string
  help?: ReactNode
  error?: string
  changed?: boolean
  children: ReactNode
}) {
  return (
    <div className="grid gap-1 py-3 sm:grid-cols-[12rem_minmax(0,1fr)] sm:items-start sm:gap-4">
      {htmlFor ? (
        <Label htmlFor={htmlFor} className="sm:pt-2">
          {label}
        </Label>
      ) : (
        <span className="text-sm font-medium sm:pt-2">{label}</span>
      )}
      <div className="flex min-w-0 flex-col gap-1">
        <div className="flex flex-wrap items-center gap-2">
          {children}
          {changed && <span className="text-xs text-warning">Changed</span>}
        </div>
        {help && <p className="text-xs text-muted-foreground">{help}</p>}
        {error && (
          <p className="flex items-center gap-1 text-xs text-destructive">
            <CircleAlert size={13} aria-hidden="true" className="shrink-0" />
            {error}
          </p>
        )}
      </div>
    </div>
  )
}
