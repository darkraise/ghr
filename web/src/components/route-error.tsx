import { useRouter, type ErrorComponentProps } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { errorText } from "@/query"

export function RouteError({ error }: ErrorComponentProps) {
  const router = useRouter()
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-4 p-8 text-center">
      <p className="text-sm text-destructive">cannot load the page: {errorText(error)}</p>
      <Button onClick={() => void router.invalidate()}>Retry</Button>
    </div>
  )
}
