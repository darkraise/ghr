import { stateIcon, TONE_TEXT } from "@/lib/state-icon"
import { stateTone, stateWord } from "@/lib/status"

const KNOWN = new Set(["success", "failure", "cancelled", "skipped", "unknown"])

// Any other conclusion shows as failed, as the Dashboard has always shown it.
// "unknown" is ghr's own conclusion for a job whose result it never learned.
export function ResultIcon({ conclusion }: { conclusion: string }) {
  const state = KNOWN.has(conclusion) ? conclusion : "failure"
  const Icon = stateIcon(state)
  return <Icon role="img" aria-label={stateWord(state)} size={15} className={`shrink-0 ${TONE_TEXT[stateTone(state)]}`} />
}
