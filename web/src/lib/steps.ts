import type { Step } from "@/api/types"

export type StepPhase = "running" | "completed" | "pending"

export function stepPhase(step: Step): StepPhase {
  if (step.status === "in_progress") return "running"
  if (step.status === "completed") return "completed"
  return "pending"
}

export interface StepTimeline {
  bars: ({ offset: number; width: number } | null)[]
  durations: (number | null)[]
}

function stepEnd(step: Step, now: number): number | null {
  if (!step.started_at) return null
  if (step.completed_at) return Date.parse(step.completed_at)
  return stepPhase(step) === "running" ? now : Date.parse(step.started_at)
}

// The axis runs from the first start to now while the runner is live, and to
// the last end once it has finished. Offsets and widths are fractions of it.
export function stepTimeline(steps: Step[], now: number, live: boolean): StepTimeline {
  const starts = steps.flatMap((s) => (s.started_at ? [Date.parse(s.started_at)] : []))
  if (starts.length === 0) return { bars: steps.map(() => null), durations: steps.map(() => null) }
  const from = Math.min(...starts)
  const ends = steps.map((s) => stepEnd(s, now))
  const to = live ? now : Math.max(from, ...ends.flatMap((e) => (e === null ? [] : [e])))
  const span = Math.max(1, to - from)
  const bars: StepTimeline["bars"] = []
  const durations: StepTimeline["durations"] = []
  steps.forEach((s, i) => {
    const end = ends[i]
    if (!s.started_at || end === null || end === undefined) {
      bars.push(null)
      durations.push(null)
      return
    }
    const start = Date.parse(s.started_at)
    bars.push({ offset: (start - from) / span, width: Math.max(0, end - start) / span })
    durations.push(Math.max(0, end - start))
  })
  return { bars, durations }
}
