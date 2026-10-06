const W = 120
const H = 32

export function Sparkline({ values, max, label }: { values: (number | null)[]; max?: number; label: string }) {
  const nums = values.filter((v): v is number => v !== null)
  const top = max ?? Math.max(1, ...nums)
  const step = values.length > 1 ? W / (values.length - 1) : W
  const segments: string[][] = [[]]
  values.forEach((v, i) => {
    if (v === null) {
      if ((segments.at(-1) ?? []).length > 0) segments.push([])
      return
    }
    const y = H - 2 - (Math.min(v, top) / top) * (H - 4)
    segments.at(-1)?.push(`${(i * step).toFixed(1)},${y.toFixed(1)}`)
  })
  return (
    <svg role="img" aria-label={label} viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="mt-2 h-8 w-full text-primary">
      {segments
        .filter((s) => s.length > 0)
        .map((s, i) => (
          <polyline key={i} fill="none" stroke="currentColor" strokeWidth="1.5" points={s.join(" ")} />
        ))}
    </svg>
  )
}
