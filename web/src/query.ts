import { MutationCache, QueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { ApiError } from "./api/client"
import { hhmm } from "./lib/format"

export function errorText(err: unknown): string {
  if (err instanceof ApiError && err.retryAt) return `${err.message} — retry after ${hhmm(err.retryAt)}`
  return err instanceof Error ? err.message : String(err)
}

// Polls do not retry: the next poll is the retry. A failed
// action shows as a toast, whichever page started it.
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
    mutationCache: new MutationCache({ onError: (err) => toast.error(errorText(err)) }),
  })
}
