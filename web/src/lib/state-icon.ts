import { Ban, Check, CircleDot, Clock, Dot, Minus, X, type LucideIcon } from "lucide-react"
import { stateTone, type Tone } from "@/lib/status"

const BY_TONE: Record<Tone, LucideIcon> = { ok: Check, bad: X, run: CircleDot, warn: Clock, accent: Dot, muted: Minus }
const BY_STATE: Record<string, LucideIcon | undefined> = { cancelled: Ban }

export const TONE_TEXT: Record<Tone, string> = {
  ok: "text-success",
  bad: "text-destructive",
  run: "text-running",
  warn: "text-warning",
  accent: "text-primary",
  muted: "text-muted-foreground",
}

export const TONE_FILL: Record<Tone, string> = {
  ok: "bg-success/15",
  bad: "bg-destructive/15",
  run: "bg-running/15",
  warn: "bg-warning/15",
  accent: "bg-primary/15",
  muted: "bg-muted-foreground/15",
}

export function stateIcon(state: string): LucideIcon {
  return BY_STATE[state.toLowerCase()] ?? BY_TONE[stateTone(state)]
}
