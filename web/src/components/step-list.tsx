import { Spinner } from "darkraise-ui/components/spinner"
import { Circle } from "lucide-react"
import type { Step } from "@/api/types"
import { ResultIcon } from "@/components/result-icon"
import { dur } from "@/lib/format"
import { stepPhase, stepTimeline } from "@/lib/steps"

function StepIcon({ step }: { step: Step }) {
  const phase = stepPhase(step)
  if (phase === "running") {
    return (
      <span className="inline-flex">
        <Spinner size="sm" label={<span className="sr-only">Running</span>} />
      </span>
    )
  }
  if (phase === "completed") return <ResultIcon conclusion={step.conclusion} />
  return <Circle role="img" aria-label="Pending" size={15} className="shrink-0 text-muted-foreground" />
}

export function StepList({ steps, now, live }: { steps: Step[]; now: number; live: boolean }) {
  const timeline = stepTimeline(steps, now, live)
  return (
    <ol aria-label="Steps" className="flex flex-col">
      {steps.map((s, i) => {
        const bar = timeline.bars[i]
        const took = timeline.durations[i]
        const pending = stepPhase(s) === "pending"
        return (
          <li
            key={s.number}
            className="grid grid-cols-[1.25rem_minmax(0,1fr)_4.5rem_minmax(6rem,35%)] items-center gap-2 border-b border-border py-1.5 text-sm last:border-b-0"
          >
            <StepIcon step={s} />
            <span className={`break-words ${pending ? "text-muted-foreground" : ""}`}>{s.name}</span>
            <span className="text-right font-mono text-xs text-muted-foreground">{took === null || took === undefined ? "" : dur(took)}</span>
            <span aria-hidden="true" className="relative h-1.5 rounded-[2px] bg-muted">
              {bar && (
                <span
                  data-bar="true"
                  className={`absolute inset-y-0 rounded-[2px] ${stepPhase(s) === "running" ? "bg-primary" : "bg-primary/60"}`}
                  style={{ left: `${bar.offset * 100}%`, width: `max(2px, ${bar.width * 100}%)` }}
                />
              )}
            </span>
          </li>
        )
      })}
    </ol>
  )
}
