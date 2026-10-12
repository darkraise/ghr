import { Spinner } from "darkraise-ui/components/spinner"
import { useEffect, useState } from "react"
import { api } from "@/api/client"
import { Brand } from "@/components/brand"

const POLL_MS = 1000
// The CSP limits fetches to the page's own origin, so a new address cannot be
// asked; the page gives ghr this long and then loads it. If it does not
// answer there, the browser's own error page says so.
const MOVE_WAIT_MS = 10_000
// Only the page's own origin is polled, so only it can be late.
const LATE_MS = 60_000

export function RestartScreen({ url, same }: { url: string; same: boolean }) {
  const [late, setLate] = useState(false)

  useEffect(() => {
    if (!same) return
    const timer = window.setTimeout(() => setLate(true), LATE_MS)
    return () => window.clearTimeout(timer)
  }, [same])

  useEffect(() => {
    const login = () => window.location.assign(`${url}/login`)
    if (!same) {
      const timer = window.setTimeout(login, MOVE_WAIT_MS)
      return () => window.clearTimeout(timer)
    }
    const ctrl = new AbortController()
    let timer = 0
    const poll = () => {
      timer = window.setTimeout(() => {
        // The old process may answer for a moment, still holding this
        // session; sessions live in memory, so the new one answers logged out.
        api.authState(ctrl.signal).then((state) => (state.authenticated ? again() : login()), again)
      }, POLL_MS)
    }
    const again = () => {
      if (!ctrl.signal.aborted) poll()
    }
    poll()
    return () => {
      ctrl.abort()
      window.clearTimeout(timer)
    }
  }, [url, same])

  return (
    <main className="flex min-h-dvh flex-col items-center justify-center gap-4 bg-background p-6 text-center text-foreground">
      <Brand />
      <p className="flex items-center gap-2 text-sm">
        <Spinner />
        <span>Restarting ghr…</span>
      </p>
      {late && (
        <div className="flex flex-col items-center gap-2 text-sm">
          <p>ghr has not answered yet.</p>
          <a href={url} className="font-mono text-primary hover:underline">
            {url}
          </a>
        </div>
      )}
    </main>
  )
}
