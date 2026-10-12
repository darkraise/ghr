import { queryOptions } from "@tanstack/react-query"
import { api } from "./client"
import { keys, POLL_SLOW } from "./hooks"

// The route guards run on every history change, so the answer stays fresh for
// one slow poll. An ended session does not wait for it: the 401 of any API
// call clears the cache and opens /login.
export const authQuery = queryOptions({
  queryKey: keys.auth,
  queryFn: ({ signal }) => api.authState(signal),
  staleTime: POLL_SLOW,
})
