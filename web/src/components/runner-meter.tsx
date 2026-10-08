// One cell per allowed runner, filled for each busy one. An unlimited
// repository (max 0) has nothing to draw.
export function RunnerMeter({ active, max }: { active: number; max: number }) {
  if (max === 0) return null
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
