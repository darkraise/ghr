import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ReactNode } from "react"
import { trackQueryClient } from "./query-clients"

export function withQuery() {
  const queryClient = trackQueryClient(new QueryClient({ defaultOptions: { queries: { retry: false } } }))
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return { queryClient, wrapper }
}
