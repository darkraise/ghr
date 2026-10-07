import { Button } from "darkraise-ui/components/button"

export function Stepper({
  label,
  value,
  text,
  min = 1,
  disabled = false,
  title,
  onChange,
}: {
  label: string
  value: number
  text?: string
  min?: number
  disabled?: boolean
  title?: string
  onChange: (next: number) => void
}) {
  return (
    <div className="inline-flex items-center gap-1" title={title}>
      <Button size="sm" variant="outline" aria-label={`Lower ${label}`} disabled={disabled || value <= min} onClick={() => onChange(value - 1)}>
        −
      </Button>
      <span className="min-w-6 text-center tabular-nums">{text ?? value}</span>
      <Button size="sm" variant="outline" aria-label={`Raise ${label}`} disabled={disabled} onClick={() => onChange(value + 1)}>
        +
      </Button>
    </div>
  )
}
