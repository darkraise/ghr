import { useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { toast } from "darkraise-ui/components/sonner"
import { useState, type FormEvent } from "react"
import { ApiError, api } from "@/api/client"
import { errorText } from "@/query"

function lengthOk(password: string): boolean {
  const n = new TextEncoder().encode(password).length
  return n >= 12 && n <= 1024
}

export function AccountCard() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [again, setAgain] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function change(e: FormEvent) {
    e.preventDefault()
    setError("")
    if (!lengthOk(next)) return setError("password must be 12 to 1024 bytes")
    if (next !== again) return setError("the passwords do not match")
    setBusy(true)
    try {
      await api.changePassword(current, next)
      toast.success("password changed")
      setCurrent("")
      setNext("")
      setAgain("")
    } catch (err) {
      setError(err instanceof ApiError && err.status === 401 ? "the current password is wrong" : errorText(err))
    } finally {
      setBusy(false)
    }
  }

  async function logout() {
    try {
      await api.logout()
    } catch {
      // a session that already ended still lands on the login page
    }
    queryClient.clear()
    await navigate({ to: "/login", ignoreBlocker: true })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Account</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <form className="flex flex-col gap-3" onSubmit={(e) => void change(e)}>
          <div className="flex flex-col gap-1">
            <Label htmlFor="current-password">Current password</Label>
            <Input
              id="current-password"
              type="password"
              autoComplete="current-password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="new-password">New password</Label>
            <Input id="new-password" type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="confirm-password">Confirm new password</Label>
            <Input
              id="confirm-password"
              type="password"
              autoComplete="new-password"
              value={again}
              onChange={(e) => setAgain(e.target.value)}
            />
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <div>
            <Button type="submit" loading={busy}>
              Change password
            </Button>
          </div>
        </form>
        <div>
          <Button variant="outline" onClick={() => void logout()}>
            Log out
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
