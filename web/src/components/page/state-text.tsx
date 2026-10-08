import { stateTone, type Tone } from "@/lib/status"

const TONE_CLASS: Record<Tone, string> = {
  accent: "text-primary",
  warn: "text-warning",
  bad: "text-destructive",
  ok: "text-success",
  muted: "text-muted-foreground",
}

const sentence = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

export function StateText({ state, label }: { state: string; label?: string }) {
  return <span className={`font-medium ${TONE_CLASS[stateTone(state)]}`}>{label ?? sentence(state)}</span>
}
