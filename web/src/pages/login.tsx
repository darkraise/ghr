import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate, useSearch } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { Spinner } from "darkraise-ui/components/spinner"
import { useState, type FormEvent } from "react"
import { ApiError, api } from "@/api/client"
import { keys } from "@/api/hooks"
import { Brand } from "@/components/brand"
import { hhmm } from "@/lib/format"
import { safeRedirect } from "@/lib/redirect"

const MIN_BYTES = 12
const MAX_BYTES = 1024

function lengthOk(password: string): boolean {
  const n = new TextEncoder().encode(password).length
  return n >= MIN_BYTES && n <= MAX_BYTES
}

function loginError(err: unknown): string {
  if (err instanceof ApiError && err.status === 429 && err.retryAt) {
    return `Too many failed logins; try again after ${hhmm(err.retryAt)}.`
  }
  return err instanceof Error ? err.message : String(err)
}

export function LoginPage() {
  const auth = useQuery({ queryKey: keys.auth, queryFn: ({ signal }) => api.authState(signal) })
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { redirect } = useSearch({ from: "/login" })
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  if (auth.isError) {
    return <p className="p-8 text-center text-sm text-destructive">cannot reach ghr: {loginError(auth.error)}</p>
  }
  if (!auth.data) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Spinner />
      </div>
    )
  }
  const setup = auth.data.setup_required

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError("")
    if (setup && !lengthOk(password)) return setError("password must be 12 to 1024 bytes")
    if (setup && password !== confirm) return setError("the passwords do not match")
    setBusy(true)
    try {
      await (setup ? api.setup(password) : api.login(password))
      await queryClient.invalidateQueries({ queryKey: keys.auth })
      await navigate({ href: safeRedirect(redirect) })
    } catch (err) {
      setError(loginError(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-6 p-4">
      <Brand />
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>{setup ? "Set a password" : "Log in"}</CardTitle>
          <CardDescription>
            {setup
              ? "No password is set. Choose one to claim this ghr."
              : "Log in to manage this runner host."}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form className="flex flex-col gap-4" onSubmit={(e) => void submit(e)}>
            <div className="flex flex-col gap-2">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                type="password"
                autoComplete={setup ? "new-password" : "current-password"}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
            {setup && (
              <div className="flex flex-col gap-2">
                <Label htmlFor="confirm">Confirm password</Label>
                <Input
                  id="confirm"
                  type="password"
                  autoComplete="new-password"
                  value={confirm}
                  onChange={(e) => setConfirm(e.target.value)}
                />
              </div>
            )}
            {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
            <Button type="submit" loading={busy}>
              {setup ? "Set password" : "Log in"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
