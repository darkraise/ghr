import { Calendar, Clock } from "lucide-react"
import type { ReactNode } from "react"

export function RunValue({ kind, children }: { kind: "time" | "duration"; children: ReactNode }) {
  const Icon = kind === "time" ? Calendar : Clock
  return (
    <span className="flex items-center gap-1.5 font-mono text-xs text-muted-foreground">
      <Icon size={13} aria-hidden="true" className="shrink-0" />
      {children}
    </span>
  )
}

export function RunGroup({ label, cols }: { label: string; cols: number }) {
  return (
    <tr className="border-b border-border bg-background">
      <th scope="rowgroup" colSpan={cols} className="px-4 py-1.5 text-left text-xs font-semibold text-muted-foreground">
        {label}
      </th>
    </tr>
  )
}

export function RunRow({
  icon,
  title,
  facts,
  values,
  actions,
  wide,
}: {
  icon: ReactNode
  title: ReactNode
  facts?: ReactNode
  values?: ReactNode
  actions?: ReactNode
  wide: boolean
}) {
  return (
    <tr className="border-b border-border last:border-b-0 hover:bg-muted/50">
      <td className="w-9 py-2.5 pr-0 pl-4 align-top">
        <span className="flex h-5 items-center">{icon}</span>
      </td>
      <td className="px-3 py-2.5 align-top">
        <span className="block text-sm font-semibold">{title}</span>
        {facts && <span className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">{facts}</span>}
        {!wide && values && <span className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5">{values}</span>}
      </td>
      {wide && (
        <td className="w-44 py-2.5 pr-4 text-right align-top">
          <span className="flex flex-col items-end gap-1">{values}</span>
        </td>
      )}
      {actions && <td className="w-20 py-2.5 pr-3 text-right align-top">{actions}</td>}
    </tr>
  )
}
