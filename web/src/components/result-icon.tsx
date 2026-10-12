import { stateIcon, TONE_TEXT } from "@/lib/state-icon"
import { resultState, stateTone, stateWord } from "@/lib/status"

export function ResultIcon({ conclusion }: { conclusion: string }) {
  const state = resultState(conclusion)
  const Icon = stateIcon(state)
  return <Icon role="img" aria-label={stateWord(state)} size={15} className={`shrink-0 ${TONE_TEXT[stateTone(state)]}`} />
}
