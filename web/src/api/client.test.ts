import { afterEach, describe, expect, it, vi } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { ApiError, api, setUnauthorizedHandler } from "./client"

afterEach(() => setUnauthorizedHandler(() => {}))

describe("api client", () => {
  it("sends X-GHR and same-origin credentials", async () => {
    const { fetchMock } = mockApi({ "GET /api/status": { epoch: "e" } })
    await api.status()
    const init = fetchMock.mock.calls[0]?.[1]
    expect(new Headers(init?.headers).get("X-GHR")).toBe("1")
    expect(init?.credentials).toBe("same-origin")
  })

  it("hands an /api 401 to the unauthorized handler", async () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    mockApi({ "GET /api/status": () => json({ error: "not logged in" }, 401) })
    await expect(api.status()).rejects.toMatchObject({ status: 401, message: "not logged in" })
    expect(onUnauthorized).toHaveBeenCalledOnce()
  })

  it("throws a wrong-password 401 from /auth without redirecting", async () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    mockApi({
      "POST /auth/login": () => json({ error: "wrong password" }, 401),
      "POST /auth/password": () => json({ error: "wrong password" }, 401),
    })
    await expect(api.login("x")).rejects.toBeInstanceOf(ApiError)
    await expect(api.changePassword("old password!", "new password!")).rejects.toMatchObject({
      status: 401,
      message: "wrong password",
    })
    expect(onUnauthorized).not.toHaveBeenCalled()
  })

  it("parses retry_at into ApiError.retryAt", async () => {
    mockApi({
      "POST /auth/login": () =>
        json({ error: "too many failed logins; retry after 2026-10-06T14:20:00Z", retry_at: "2026-10-06T14:20:00Z" }, 429),
    })
    const err = await api.login("x").catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(429)
    expect((err as ApiError).retryAt?.toISOString()).toBe("2026-10-06T14:20:00.000Z")
  })

  it("keeps a plain-text error body as the message", async () => {
    mockApi({ "GET /api/status": () => new Response("404 page not found\n", { status: 404 }) })
    await expect(api.status()).rejects.toMatchObject({ status: 404, message: "404 page not found" })
  })

  it("sends JSON bodies and builds query strings", async () => {
    const { calls } = mockApi({ "POST /auth/setup": () => noContent(), "GET /api/history": [] })
    await api.setup("correct horse battery")
    await api.history("darkmem", "", 200)
    expect(calls[0]?.body).toEqual({ password: "correct horse battery" })
    expect(calls[1]?.search).toBe("?repo=darkmem&conclusion=&limit=200")
  })

  it("escapes runner IDs and cursors", async () => {
    const { calls } = mockApi({ "GET /api/runners/a%2Fb/log": { data: "", next: "" } })
    await api.log("a/b", "x;y")
    expect(calls[0]?.search).toBe("?cursor=x%3By")
  })
})
