import "@testing-library/jest-dom/vitest"
import { cleanup, configure } from "@testing-library/react"
import { afterEach, vi } from "vitest"

// darkraise-ui's ThemeProvider calls window.matchMedia without a guard, and
// jsdom implements neither matchMedia, scrollTo nor scrollIntoView.
if (!window.matchMedia) {
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    configurable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }),
  })
}
window.scrollTo = () => {}
Element.prototype.scrollIntoView = () => {}

// A loaded machine can take over a second to settle a poll; findBy queries
// get 3 seconds, under Vitest's 5-second test timeout.
configure({ asyncUtilTimeout: 3000 })

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})
