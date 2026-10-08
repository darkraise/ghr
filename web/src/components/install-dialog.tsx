import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "darkraise-ui/components/dialog"
import { Input } from "darkraise-ui/components/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useToolchainChoices } from "@/api/hooks"
import { ErrorLine } from "@/components/page/error-line"
import { errorText } from "@/query"

const TOOLS = [
  ["node", "Node.js"],
  ["python", "Python"],
  ["go", "Go"],
  ["java", "Java (Temurin)"],
  ["dotnet", ".NET SDK"],
] as const

export function InstallDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) onClose()
      }}
    >
      <DialogContent>{open && <InstallForm onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}

function InstallForm({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient()
  const [tool, setTool] = useState("node")
  const [filter, setFilter] = useState("")
  const [picked, setPicked] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const choices = useToolchainChoices(tool, true)
  const target = picked ?? filter.trim()
  const all = choices.data ?? []
  const shown = all.filter((c) => c.version.toLowerCase().includes(filter.trim().toLowerCase()))

  async function submit() {
    setError("")
    setBusy(true)
    try {
      await api.installToolchain(tool, target)
      toast.success(`Queued: install ${tool} ${target}`)
      void queryClient.invalidateQueries({ queryKey: keys.storage })
      onClose()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Install toolchain</DialogTitle>
      </DialogHeader>
      <DialogBody className="flex flex-col gap-3">
        <Select
          value={tool}
          disabled={busy}
          onValueChange={(v) => {
            setTool(v)
            setPicked(null)
            setFilter("")
          }}
        >
          <SelectTrigger aria-label="Tool">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {TOOLS.map(([value, label]) => (
              <SelectItem key={value} value={value}>
                {label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <p className="text-sm">
          {target ? <strong className="font-mono">{target}</strong> : <span className="text-muted-foreground">Pick one below, or type a version</span>}
        </p>
        <Input
          aria-label="Version"
          placeholder="Type to filter"
          value={filter}
          onChange={(e) => {
            setFilter(e.target.value)
            setPicked(null)
          }}
        />
        {choices.isPending ? (
          <Spinner label="Loading versions" />
        ) : choices.isError ? (
          <ErrorLine onRetry={() => void choices.refetch()}>{errorText(choices.error)}</ErrorLine>
        ) : shown.length === 0 ? (
          <p className="text-sm text-muted-foreground">{all.length === 0 ? "Nothing to pick" : "No match"}</p>
        ) : (
          <ul className="flex max-h-48 flex-col overflow-auto">
            {shown.map((c) => (
              <li key={c.spec}>
                <button
                  type="button"
                  aria-pressed={picked === c.spec}
                  className={`flex w-full items-center gap-2 rounded px-2 py-1 text-left text-sm hover:bg-muted ${picked === c.spec ? "bg-muted" : ""}`}
                  onClick={() => setPicked(c.spec)}
                >
                  <span className="font-mono">{c.version}</span>
                  {c.lts && <span className="text-xs text-muted-foreground">lts</span>}
                </button>
              </li>
            ))}
          </ul>
        )}
        {error && <ErrorLine>{error}</ErrorLine>}
      </DialogBody>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button loading={busy} disabled={busy || target === ""} onClick={() => void submit()}>
          Install
        </Button>
      </DialogFooter>
    </>
  )
}
