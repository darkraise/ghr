import { onTestFinished } from "vitest"

// jsdom has no layout, so a test picks a width: (min-width: Npx) queries
// match against it, and resize() tells the listeners. The default stub in
// setup.ts comes back after the test.
export function setViewport(width: number): { resize(width: number): void } {
  let current = width
  const listeners = new Set<() => void>()
  const original = window.matchMedia
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    configurable: true,
    value: (query: string) => {
      const min = /\(min-width:\s*(\d+)px\)/.exec(query)
      return {
        get matches() {
          return min ? current >= Number(min[1]) : false
        },
        media: query,
        onchange: null,
        addEventListener: (_type: string, fn: () => void) => listeners.add(fn),
        removeEventListener: (_type: string, fn: () => void) => listeners.delete(fn),
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }
    },
  })
  onTestFinished(() => {
    Object.defineProperty(window, "matchMedia", { writable: true, configurable: true, value: original })
  })
  return {
    resize(next: number) {
      current = next
      for (const fn of listeners) fn()
    },
  }
}
