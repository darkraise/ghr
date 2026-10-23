import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "darkraise-ui/components/dialog"
import { Input } from "darkraise-ui/components/input"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { useEffect, useState } from "react"
import { api } from "@/api/client"
import { keys, usePackageSearch } from "@/api/hooks"
import type { PackageInfo } from "@/api/types"
import { ErrorLine } from "@/components/page/error-line"
import { validTerm } from "@/lib/packages"
import { useMounted } from "@/lib/use-mounted"
import { errorText } from "@/query"

// Each keystroke would otherwise run apt-cache on the host.
export const SEARCH_DELAY = 300

export function PackageAddDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) onClose()
      }}
    >
      <DialogContent>{open && <AddForm onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}

function stateText(p: PackageInfo): string {
  if (p.managed) return "managed"
  return p.installed ? "installed" : ""
}

function AddForm({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient()
  const mounted = useMounted()
  const [text, setText] = useState("")
  const [term, setTerm] = useState("")
  const [picked, setPicked] = useState<PackageInfo | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const typed = text.trim()
  useEffect(() => {
    const timer = setTimeout(() => setTerm(typed), SEARCH_DELAY)
    return () => clearTimeout(timer)
  }, [typed])
  const search = usePackageSearch(term)
  const results = search.data ?? []
  const adopt = picked?.installed === true

  async function submit() {
    if (!picked) return
    setError("")
    setBusy(true)
    try {
      const result = await api.addPackages([picked.name])
      toast.success(`Queued: ${result.adopt.length > 0 ? "adopt" : "install"} ${picked.name}`)
      void queryClient.invalidateQueries({ queryKey: keys.packages })
      void queryClient.invalidateQueries({ queryKey: keys.storage })
      if (mounted.current) onClose()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  let list
  if (typed === "") {
    list = <p className="text-sm text-muted-foreground">Type part of a package name</p>
  } else if (!validTerm(typed)) {
    list = <p className="text-sm text-muted-foreground">Type 2 to 64 characters of a-z, 0-9, +, . and -</p>
  } else if (term !== typed || search.isPending) {
    list = <Spinner label="Searching" />
  } else if (search.isError) {
    list = <ErrorLine onRetry={() => void search.refetch()}>{errorText(search.error)}</ErrorLine>
  } else if (results.length === 0) {
    list = <p className="text-sm text-muted-foreground">No package matches</p>
  } else {
    list = (
      <ul aria-label="Search results" className="flex max-h-64 flex-col overflow-auto">
        {results.map((p) => (
          <li key={p.name}>
            <button
              type="button"
              aria-label={[p.name, stateText(p)].filter(Boolean).join(", ")}
              aria-pressed={picked?.name === p.name}
              disabled={p.managed === true}
              className={`flex w-full items-baseline gap-2 rounded px-2 py-1 text-left text-sm hover:bg-muted disabled:opacity-60 disabled:hover:bg-transparent ${picked?.name === p.name ? "bg-muted" : ""}`}
              onClick={() => setPicked(p)}
            >
              <span className="font-mono">{p.name}</span>
              {stateText(p) && <span className="text-xs text-muted-foreground">{stateText(p)}</span>}
              {p.description && <span className="min-w-0 truncate text-muted-foreground">{p.description}</span>}
            </button>
          </li>
        ))}
      </ul>
    )
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Add package</DialogTitle>
      </DialogHeader>
      <DialogBody className="flex flex-col gap-3">
        <Input
          aria-label="Package name"
          placeholder="Search the apt repositories"
          value={text}
          onChange={(e) => {
            setText(e.target.value)
            setPicked(null)
          }}
        />
        {list}
        {picked && (
          <p className="text-sm">
            {adopt ? (
              <>
                <strong className="font-mono">{picked.name}</strong> is installed already. ghr adopts it: it joins the managed list and nothing is installed.
              </>
            ) : (
              <>
                ghr installs <strong className="font-mono">{picked.name}</strong> from the host's apt sources.
              </>
            )}
          </p>
        )}
        {error && <ErrorLine>{error}</ErrorLine>}
      </DialogBody>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button loading={busy} disabled={busy || picked === null} onClick={() => void submit()}>
          {adopt ? "Adopt" : "Install"}
        </Button>
      </DialogFooter>
    </>
  )
}
