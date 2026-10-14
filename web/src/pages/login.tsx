import { useQuery } from "@tanstack/react-query"
import { useSearch } from "@tanstack/react-router"
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
import { passwordLengthOk } from "@/lib/password"
import { safeRedirect } from "@/lib/redirect"

function loginError(err: unknown): string {
  if (err instanceof ApiError && err.status === 429 && err.retryAt) {
    return `Too many failed logins; try again after ${hhmm(err.retryAt)}.`
  }
  return err instanceof Error ? err.message : String(err)
}

export function LoginPage() {
  const auth = useQuery({ queryKey: keys.auth, queryFn: ({ signal }) => api.authState(signal) })
  const { redirect } = useSearch({ from: "/login" })
  const [code, setCode] = useState("")
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
    if (setup && !passwordLengthOk(password)) return setError("password must be 12 to 1024 characters")
    if (setup && password !== confirm) return setError("the passwords do not match")
    setBusy(true)
    try {
      await (setup ? api.setup(code, password) : api.login(password))
      // A full load rather than a route change: a daemon restart for a deploy
      // ends every session, and this way the tab sent here picks up the web
      // build that came with it. The button stays busy until the page goes.
      window.location.assign(safeRedirect(redirect))
    } catch (err) {
      setError(loginError(err))
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
              ? "No password is set. Enter the setup code from this host and choose one to claim this ghr."
              : "Log in to manage this runner host."}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form className="flex flex-col gap-4" onSubmit={(e) => void submit(e)}>
            {setup && (
              <div className="flex flex-col gap-2">
                <Label htmlFor="setup-code">Setup code</Label>
                <Input
                  id="setup-code"
                  autoComplete="one-time-code"
                  spellCheck={false}
                  aria-describedby="setup-code-help"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                />
                <p id="setup-code-help" className="text-sm text-muted-foreground">
                  Printed by setup.sh. On the host, run <code className="font-mono">ghr code</code>.
                </p>
              </div>
            )}
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
