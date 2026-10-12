import { afterEach, describe, expect, it, vi } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { ApiError, api, setUnauthorizedHandler } from "./client"

afterEach(() => setUnauthorizedHandler(() => {}))

describe("api client", () => {
  it("sends X-GHR and same-origin credentials", async () => {
    const { fetchMock } = mockApi({ "GET /api/status": { epoch: "e" } })
    await api.status()
    const init = fetchMock.mock.calls[0]?.[1]
    expect(init?.method).toBe("GET")
    expect(new Headers(init?.headers).get("X-GHR")).toBe("1")
    expect(new Headers(init?.headers).get("Content-Type")).toBeNull()
    expect(init?.credentials).toBe("same-origin")
  })

  it("hands an /api 401 to the unauthorized handler", async () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    mockApi({ "GET /api/status": () => json({ error: "not logged in" }, 401) })
    await expect(api.status()).rejects.toMatchObject({ status: 401, message: "not logged in" })
    expect(onUnauthorized).toHaveBeenCalledOnce()
  })

  it("throws a wrong login or setup code 401 without redirecting", async () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    mockApi({
      "POST /auth/login": () => json({ error: "wrong password" }, 401),
      "POST /auth/setup": () => json({ error: "wrong setup code; run on the host: ghr code" }, 401),
    })
    await expect(api.login("x")).rejects.toMatchObject({ status: 401, message: "wrong password" })
    await expect(api.setup("x", "correct horse battery")).rejects.toMatchObject({
      status: 401,
      message: "wrong setup code; run on the host: ghr code",
    })
    expect(onUnauthorized).not.toHaveBeenCalled()
  })

  it("throws a wrong current password 403 without redirecting", async () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    mockApi({ "POST /auth/password": () => json({ error: "wrong password" }, 403) })
    await expect(api.changePassword("old password!", "new password!")).rejects.toMatchObject({
      status: 403,
      message: "wrong password",
    })
    expect(onUnauthorized).not.toHaveBeenCalled()
  })

  it("hands a password change 401 to the unauthorized handler", async () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    mockApi({ "POST /auth/password": () => json({ error: "not logged in" }, 401) })
    await expect(api.changePassword("old password!", "new password!")).rejects.toMatchObject({
      status: 401,
      message: "not logged in",
    })
    expect(onUnauthorized).toHaveBeenCalledOnce()
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

  it.each<[string, string, string, string]>([
    ["an HTML body", "<html><body><h1>502 Bad Gateway</h1><p>nginx</p></body></html>", "text/html", "502 Bad Gateway"],
    ["JSON with no error field", '{"detail":"upstream timed out"}', "application/json", "502 Bad Gateway"],
    ["an empty plain-text body", "", "text/plain", "502 Bad Gateway"],
    ["a long plain-text body", "x".repeat(300), "text/plain; charset=utf-8", "x".repeat(200)],
    ["JSON with an error field and the wrong type", '{"error":"upstream is down"}', "text/html", "upstream is down"],
  ])("reads the error message from %s", async (_name, body, contentType, want) => {
    mockApi({
      "GET /api/status": () => new Response(body, { status: 502, statusText: "Bad Gateway", headers: { "Content-Type": contentType } }),
    })
    await expect(api.status()).rejects.toMatchObject({ status: 502, message: want })
  })

  it("accepts an empty body on a 200", async () => {
    mockApi({ "POST /api/pause-all": () => new Response(null, { status: 200 }) })
    await expect(api.pauseAll()).resolves.toBeUndefined()
  })

  it("sends JSON bodies and builds query strings", async () => {
    const { calls } = mockApi({ "POST /auth/setup": () => noContent(), "GET /api/history": [] })
    await api.setup("ABCDE-FGHJK-MNPQR-STVWX", "correct horse battery")
    await api.history("darkmem", "", "", 200)
    expect(calls[0]).toMatchObject({
      method: "POST",
      body: { code: "ABCDE-FGHJK-MNPQR-STVWX", password: "correct horse battery" },
      contentType: "application/json",
    })
    expect(calls[1]?.method).toBe("GET")
    expect(calls[1]?.search).toBe("?repo=darkmem&conclusion=&limit=200")
  })

  it("sends since only when it is set", async () => {
    const { calls } = mockApi({ "GET /api/history": [] })
    await api.history("", "failure", "2026-10-01T18:00:00+07:00", 500)
    expect(calls[0]?.search).toBe("?repo=&conclusion=failure&since=2026-10-01T18%3A00%3A00%2B07%3A00&limit=500")
  })

  it("escapes runner IDs and cursors", async () => {
    const { calls } = mockApi({ "GET /api/runners/a%2Fb/log": { data: "", next: "" } })
    await api.log("a/b", "x;y")
    expect(calls[0]?.search).toBe("?cursor=x%3By")
  })

  it("asks for a restart with an empty POST that carries X-GHR", async () => {
    const { calls, fetchMock } = mockApi({ "POST /api/restart": () => new Response(null, { status: 202 }) })
    await expect(api.restart()).resolves.toBeUndefined()
    expect(calls).toEqual([{ method: "POST", path: "/api/restart", search: "", body: undefined, contentType: undefined }])
    expect(new Headers(fetchMock.mock.calls[0]?.[1]?.headers).get("X-GHR")).toBe("1")
  })
})
