import { createMemoryHistory } from "@tanstack/react-router"
import { render } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { App } from "@/app"
import { createQueryClient } from "@/query"
import { createAppRouter } from "@/router"

export function renderApp(path: string) {
  const queryClient = createQueryClient()
  const router = createAppRouter({ queryClient, history: createMemoryHistory({ initialEntries: [path] }) })
  const user = userEvent.setup()
  const view = render(<App router={router} queryClient={queryClient} />)
  return { ...view, router, queryClient, user }
}
