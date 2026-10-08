import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { toast } from "darkraise-ui/components/sonner"
import { useState, type FormEvent } from "react"
import { ApiError, api } from "@/api/client"
import { ErrorLine } from "@/components/page/error-line"
import { Field, FieldGroup } from "@/components/page/field"
import { errorText } from "@/query"

function lengthOk(password: string): boolean {
  const n = new TextEncoder().encode(password).length
  return n >= 12 && n <= 1024
}

export function AccountSection() {
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [again, setAgain] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function change(e: FormEvent) {
    e.preventDefault()
    setError("")
    if (!lengthOk(next)) return setError("Password must be 12 to 1024 bytes")
    if (next !== again) return setError("The passwords do not match")
    setBusy(true)
    try {
      await api.changePassword(current, next)
      toast.success("Password changed")
      setCurrent("")
      setNext("")
      setAgain("")
    } catch (err) {
      setError(err instanceof ApiError && err.status === 401 ? "The current password is wrong" : errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <FieldGroup id="account" title="Account" note="Changing the password takes effect at once, outside the save bar.">
      <form className="divide-y divide-border" onSubmit={(e) => void change(e)}>
        <Field label="Current password" htmlFor="current-password">
          <Input
            id="current-password"
            type="password"
            autoComplete="current-password"
            className="max-w-xs"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
          />
        </Field>
        <Field label="New password" htmlFor="new-password" help="12 to 1024 bytes">
          <Input id="new-password" type="password" autoComplete="new-password" className="max-w-xs" value={next} onChange={(e) => setNext(e.target.value)} />
        </Field>
        <Field label="Confirm new password" htmlFor="confirm-password">
          <Input
            id="confirm-password"
            type="password"
            autoComplete="new-password"
            className="max-w-xs"
            value={again}
            onChange={(e) => setAgain(e.target.value)}
          />
        </Field>
        {error && (
          <div className="py-3">
            <ErrorLine>{error}</ErrorLine>
          </div>
        )}
        <div className="py-3">
          <Button type="submit" loading={busy}>
            Change password
          </Button>
        </div>
      </form>
    </FieldGroup>
  )
}
