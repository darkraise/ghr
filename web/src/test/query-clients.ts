import type { QueryClient } from "@tanstack/react-query"

const clients = new Set<QueryClient>()

export function trackQueryClient(client: QueryClient): QueryClient {
  clients.add(client)
  return client
}

// A query left fetching or cached outlives its test, and can settle after
// the test file's jsdom is gone.
export async function releaseQueryClients(): Promise<void> {
  const made = [...clients]
  clients.clear()
  for (const client of made) {
    await client.cancelQueries()
    client.clear()
  }
}
