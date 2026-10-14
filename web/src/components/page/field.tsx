import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { CircleAlert } from "lucide-react"
import { type ComponentProps, type ReactNode, useId } from "react"
import { FieldAriaContext, useFieldAria } from "./field-aria"

// The heading takes focus when the section index jumps here, so it carries
// tabIndex -1 and an id the index can find.
export function FieldGroup({ id, title, note, children }: { id: string; title: string; note?: string; children: ReactNode }) {
  return (
    <section id={id} aria-labelledby={`${id}-title`} className="min-w-0 scroll-mt-4 overflow-hidden rounded-md border border-border bg-card">
      <div className="border-b border-border bg-background px-4 py-2.5">
        <h2 id={`${id}-title`} tabIndex={-1} className="text-sm font-semibold">
          {title}
        </h2>
        {note && <p className="mt-0.5 text-xs text-muted-foreground">{note}</p>}
      </div>
      <div className="divide-y divide-border px-4">{children}</div>
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
  const id = useId()
  const describedBy = [help && `${id}-help`, error && `${id}-error`].filter(Boolean).join(" ")
  return (
    <FieldAriaContext.Provider value={{ "aria-describedby": describedBy || undefined, "aria-invalid": error ? true : undefined }}>
      <div className="grid gap-1 py-3 sm:grid-cols-[12rem_minmax(0,1fr)] sm:items-baseline sm:gap-4">
        {htmlFor ? (
          <Label htmlFor={htmlFor}>{label}</Label>
        ) : (
          <span className="text-sm font-semibold">{label}</span>
        )}
        <div className="flex min-w-0 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-2">
            {children}
            {changed && <span className="text-xs text-warning">Changed</span>}
          </div>
          {help && (
            <p id={`${id}-help`} className="text-xs text-muted-foreground">
              {help}
            </p>
          )}
          {error && (
            <p id={`${id}-error`} className="flex items-center gap-1 text-xs text-destructive">
              <CircleAlert size={13} aria-hidden="true" className="shrink-0" />
              {error}
            </p>
          )}
        </div>
      </div>
    </FieldAriaContext.Provider>
  )
}

export function FieldInput({ "aria-describedby": describedBy, ...props }: ComponentProps<typeof Input>) {
  const field = useFieldAria()
  const linked = [field["aria-describedby"], describedBy].filter(Boolean).join(" ")
  return <Input {...field} {...props} aria-describedby={linked || undefined} />
}
