import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { useState, type FormEvent } from "react"
import { ApiError, api } from "@/api/client"
import { ErrorLine } from "@/components/page/error-line"
import { Field, FieldGroup, FieldInput } from "@/components/page/field"
import { FIELD_WIDTH } from "@/lib/field-width"
import { hhmm } from "@/lib/format"
import { passwordLengthOk } from "@/lib/password"
import { errorText } from "@/query"

function changeError(err: unknown): string {
  if (err instanceof ApiError && err.status === 403) return "The current password is wrong"
  if (err instanceof ApiError && err.status === 429 && err.retryAt) {
    return `Too many failed attempts; try again after ${hhmm(err.retryAt)}.`
  }
  return errorText(err)
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
    if (!passwordLengthOk(next)) return setError("Password must be 12 to 1024 characters")
    if (next !== again) return setError("The passwords do not match")
    setBusy(true)
    try {
      await api.changePassword(current, next)
      toast.success("Password changed")
      setCurrent("")
      setNext("")
      setAgain("")
    } catch (err) {
      setError(changeError(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <FieldGroup id="account" title="Password" note="Changing the password takes effect at once, outside the save bar.">
      <form className="divide-y divide-border" onSubmit={(e) => void change(e)}>
        <Field label="Current password" htmlFor="current-password">
          <FieldInput
            id="current-password"
            type="password"
            autoComplete="current-password"
            className={FIELD_WIDTH}
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
          />
        </Field>
        <Field label="New password" htmlFor="new-password" help="12 to 1024 characters">
          <FieldInput id="new-password" type="password" autoComplete="new-password" className={FIELD_WIDTH} value={next} onChange={(e) => setNext(e.target.value)} />
        </Field>
        <Field label="Confirm new password" htmlFor="confirm-password">
          <FieldInput
            id="confirm-password"
            type="password"
            autoComplete="new-password"
            className={FIELD_WIDTH}
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
