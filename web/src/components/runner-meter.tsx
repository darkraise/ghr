// One cell per allowed runner, filled for each busy one; past this many the
// cells are too narrow to count, so one bar shows the share instead. An
// unlimited repository (max 0) has nothing to draw.
const MAX_CELLS = 16

export function RunnerMeter({ active, max }: { active: number; max: number }) {
  if (max === 0) return null
  if (max > MAX_CELLS) {
    return (
      <span aria-hidden="true" data-slot="runner-track" className="flex h-2.5 w-16 overflow-hidden rounded-[1px] bg-muted">
        {active > 0 && <span className="min-w-px bg-primary" style={{ width: `${(Math.min(active, max) / max) * 100}%` }} />}
      </span>
    )
  }
  return (
    <span aria-hidden="true" className="flex gap-0.5">
      {Array.from({ length: max }, (_, i) => (
        <span
          key={i}
          data-slot="runner"
          data-filled={i < active ? "true" : undefined}
          className={`h-2.5 w-1.5 rounded-[1px] ${i < active ? "bg-primary" : "bg-muted"}`}
        />
      ))}
    </span>
  )
}
