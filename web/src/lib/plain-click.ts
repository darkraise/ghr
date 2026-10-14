import type { MouseEvent } from "react"

// A click the app handles itself; any other is left to the browser, which
// opens the link in a new tab or window.
export function plainClick(e: MouseEvent): boolean {
  return e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey
}
