import { vi } from "vitest"

export interface Call {
  method: string
  path: string
  search: string
  body: unknown
  contentType: string | undefined
}

type Handler = (req: { url: URL; method: string; body: unknown }) => unknown

export function json(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } })
}

export function noContent(): Response {
  return new Response(null, { status: 204 })
}

function parseBody(body: BodyInit | null | undefined): unknown {
  if (typeof body !== "string") return undefined
  try {
    return JSON.parse(body) as unknown
  } catch {
    return body
  }
}

// Routes are keyed "<METHOD> <path>". A value is a JSON body, or a handler
// returning (or resolving to) a JSON body or a Response; a Response body can
// be read once, so a route that answers with a Response must be a handler.
export function mockApi(routes: Record<string, unknown>) {
  const calls: Call[] = []
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const raw = typeof input === "string" ? input : input instanceof URL ? input.href : input.url
    const url = new URL(raw, "http://localhost")
    const method = (init?.method ?? "GET").toUpperCase()
    const body = parseBody(init?.body)
    const contentType = (init?.headers as Record<string, string> | undefined)?.["Content-Type"]
    calls.push({ method, path: url.pathname, search: url.search, body, contentType })
    const route = routes[`${method} ${url.pathname}`]
    if (route === undefined) return json({ error: `no mock for ${method} ${url.pathname}` }, 404)
    const out = typeof route === "function" ? await (route as Handler)({ url, method, body }) : route
    return out instanceof Response ? out : json(out)
  })
  vi.stubGlobal("fetch", fetchMock)
  return { calls, fetchMock }
}
