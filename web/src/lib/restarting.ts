import { useSyncExternalStore } from "react"

export interface Restarting {
  url: string
  same: boolean
}

let current: Restarting | null = null
const listeners = new Set<() => void>()

function emit() {
  for (const fn of listeners) fn()
}

function subscribe(fn: () => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

export function startRestarting(r: Restarting): void {
  current = r
  emit()
}

// Tests end a restart so the next test starts with the app.
export function clearRestarting(): void {
  current = null
  emit()
}

export function useRestarting(): Restarting | null {
  return useSyncExternalStore(subscribe, () => current)
}
