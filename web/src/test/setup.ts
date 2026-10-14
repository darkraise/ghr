import "@testing-library/jest-dom/vitest"
import { cleanup, configure } from "@testing-library/react"
import { toast } from "darkraise-ui/components/sonner"
import { afterEach, vi } from "vitest"
import { liveConfig } from "@/api/live"
import { clearRestarting } from "@/lib/restarting"
import { releaseQueryClients } from "./query-clients"

// The change stream would add a fetch to every rendered app, and many tests
// count fetch calls; the stream's own tests turn it back on.
liveConfig.enabled = false

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

// The kit keeps its toasts in a module store and announces them in a live
// region on document.body; both outlive a test, so a toast from one test
// would otherwise satisfy a later test's wait for the same text.
afterEach(async () => {
  cleanup()
  toast.dismiss()
  document.querySelectorAll("body > [aria-live]").forEach((region) => region.remove())
  clearRestarting()
  await releaseQueryClients()
  vi.unstubAllGlobals()
})
