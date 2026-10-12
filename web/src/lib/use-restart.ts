import { useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { useConfig, useStatus } from "@/api/hooks"
import type { Confirm } from "@/components/confirm-dialog"
import { restartPlan, type RestartPlan } from "@/lib/restart"
import { startRestarting } from "@/lib/restarting"
import { errorText } from "@/query"

const BODY = "Running jobs keep running. The web UI is down for a few seconds and you will need to log in again."

function planText(plan: RestartPlan): string {
  switch (plan.kind) {
    case "off":
      return "The web UI turns off."
    case "moved":
      return `The web UI moves to ${plan.url}.`
    case "proxy":
      return `If a proxy forwards to ghr, point it at port ${plan.port ?? ""}.`
    case "same":
      return ""
  }
}

// ready is false until the config has loaded, and while its last read
// failed: the confirmation names where the web UI goes, which needs the
// saved web.listen.
export function useRestart(): { ask(): void; confirm: Confirm | null; close(): void; busy: boolean; ready: boolean } {
  const status = useStatus()
  const config = useConfig()
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const [busy, setBusy] = useState(false)

  async function restart(url: string) {
    setBusy(true)
    try {
      await api.restart()
    } catch (err) {
      setBusy(false)
      toast.error(errorText(err))
      return
    }
    // The restart ends the session: a poll or the change stream would meet a
    // 401 and send the page to the login form before ghr is back.
    queryClient.clear()
    startRestarting({ url, same: url === window.location.origin })
  }

  const ready = config.data !== undefined && !config.isError

  function ask() {
    const cfg = config.data
    if (!cfg || config.isError) return
    const state = status.data?.restart
    const running = state?.web_listen
    const listenPending = state?.pending?.includes("web.listen") ?? false
    const saved = listenPending ? (cfg.web?.listen ?? "") : running
    const plan = restartPlan(window.location, running, saved)
    const extra = planText(plan)
    setConfirm({ title: "Restart ghr?", body: extra ? `${BODY} ${extra}` : BODY, action: "Restart", run: () => void restart(plan.url) })
  }

  return { ask, confirm, close: () => setConfirm(null), busy, ready }
}
