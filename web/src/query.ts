import { MutationCache, QueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { ApiError } from "./api/client"
import { hhmm } from "./lib/format"

// The retry time is its own sentence, so callers can end the text with their
// own punctuation ("…. Retrying.") without doubling a period.
export function errorText(err: unknown): string {
  const text = err instanceof Error ? err.message : String(err)
  if (err instanceof ApiError && err.retryAt) return `${text.replace(/\.$/, "")}. Retry after ${hhmm(err.retryAt)}`
  return text
}

// Polls do not retry: the next poll is the retry. A failed
// action shows as a toast, whichever page started it.
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
    mutationCache: new MutationCache({ onError: (err) => toast.error(errorText(err)) }),
  })
}
