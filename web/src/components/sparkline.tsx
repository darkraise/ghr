const W = 120
const H = 32

export function Sparkline({
  values,
  max,
  label,
  step = false,
  className = "text-primary",
}: {
  values: (number | null)[]
  max?: number
  label: string
  step?: boolean
  className?: string
}) {
  const nums = values.filter((v): v is number => v !== null)
  const top = max ?? Math.max(1, ...nums)
  const dx = values.length > 1 ? W / (values.length - 1) : W
  const runs: [number, number][][] = [[]]
  values.forEach((v, i) => {
    const run = runs.at(-1) ?? []
    if (v === null) {
      if (run.length > 0) runs.push([])
      return
    }
    const x = i * dx
    const y = H - 2 - (Math.min(v, top) / top) * (H - 4)
    const last = run.at(-1)
    if (step && last) run.push([x, last[1]])
    run.push([x, y])
  })
  return (
    <svg role="img" aria-label={label} viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className={`mt-2 h-8 w-full ${className}`}>
      {runs
        .filter((r) => r.length > 0)
        .map((r, i) => (
          <polyline
            key={i}
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            vectorEffect="non-scaling-stroke"
            points={r.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(" ")}
          />
        ))}
    </svg>
  )
}
