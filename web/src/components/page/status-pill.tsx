import { stateIcon, TONE_FILL, TONE_TEXT } from "@/lib/state-icon"
import { stateTone, stateWord } from "@/lib/status"

// The word stays in the text colour: a coloured word on its own tint falls
// under 4.5:1 for light-mode Running.
export function StatusPill({ state, label }: { state: string; label?: string }) {
  const tone = stateTone(state)
  const Icon = stateIcon(state)
  return (
    <span className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-semibold whitespace-nowrap ${TONE_FILL[tone]}`}>
      <Icon size={13} aria-hidden="true" className={`shrink-0 ${TONE_TEXT[tone]}`} />
      <span className="text-foreground">{label ?? stateWord(state)}</span>
    </span>
  )
}
