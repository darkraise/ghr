import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useStatus } from "@/api/hooks"
import { Field, FieldGroup } from "@/components/page/field"
import { plural } from "@/lib/format"
import { errorText } from "@/query"

function reloadText(warnings: number): string {
  if (warnings === 0) return "Config reloaded"
  return `Config reloaded with ${plural(warnings, "warning")}. See Events on the Dashboard.`
}

export function ReloadConfig() {
  const status = useStatus()
  const queryClient = useQueryClient()
  const [reloading, setReloading] = useState(false)

  async function reload() {
    setReloading(true)
    try {
      toast.success(reloadText((await api.reload()).length))
    } catch (err) {
      toast.error(`Reload rejected: ${errorText(err)}`)
    } finally {
      setReloading(false)
      await Promise.all([keys.status, ["events"], keys.config, keys.token].map((queryKey) => queryClient.invalidateQueries({ queryKey })))
    }
  }

  return (
    <FieldGroup id="config-file" title="Config file" note="This acts at once, outside the save bar.">
      <Field label="Config" help="Reads config.yaml again. Warnings appear under Events on the Dashboard.">
        <Button variant="outline" disabled={status.isError} loading={reloading} onClick={() => void reload()}>
          Reload config.yaml
        </Button>
      </Field>
    </FieldGroup>
  )
}
