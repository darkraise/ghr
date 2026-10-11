import { useQuery, type QueryClient } from "@tanstack/react-query"
import { getConfig, renderHook } from "@testing-library/react"
import { describe, expect, it, onTestFinished } from "vitest"
import { mockApi } from "./api"
import { authedRoutes } from "./fixtures"
import { withQuery } from "./query"
import { renderApp } from "./render"
import { renderRoutes } from "./routes"

describe("test setup", () => {
  it("gives async queries 3 seconds and stubs scrollIntoView", () => {
    expect(getConfig().asyncUtilTimeout).toBe(3000)
    expect(() => document.createElement("div").scrollIntoView()).not.toThrow()
  })

  // onTestFinished runs after the afterEach hooks, so it sees what they left.
  const helpers: [string, () => QueryClient][] = [
    ["renderApp", () => renderApp("/").queryClient],
    ["renderRoutes", () => renderRoutes({ "/": () => null }, "/").queryClient],
    ["withQuery", () => withQuery().queryClient],
  ]

  it.each(helpers)("empties the query client %s made once the test is over", (_name, make) => {
    mockApi(authedRoutes())
    const client = make()
    client.setQueryData(["kept"], 1)
    onTestFinished(() => expect(client.getQueryCache().getAll()).toEqual([]))
  })

  it("cancels a query still fetching when the test ends", () => {
    const { queryClient, wrapper } = withQuery()
    renderHook(() => useQuery({ queryKey: ["slow"], queryFn: () => new Promise(() => {}) }), { wrapper })
    const query = queryClient.getQueryCache().find({ queryKey: ["slow"] })
    expect(query?.state.fetchStatus).toBe("fetching")
    onTestFinished(() => expect(query?.state.fetchStatus).toBe("idle"))
  })
})
