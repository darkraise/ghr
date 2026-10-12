import { describe, expect, it } from "vitest"
import { restartPlan } from "./restart"

function page(origin: string) {
  const u = new URL(origin)
  return { protocol: u.protocol, hostname: u.hostname, port: u.port, origin: u.origin }
}

describe("restartPlan", () => {
  it("keeps the page's origin when web.listen is not pending", () => {
    expect(restartPlan(page("http://192.168.0.99:8080"), "0.0.0.0:8080", "0.0.0.0:8080")).toEqual({ kind: "same", url: "http://192.168.0.99:8080" })
  })

  it("turns the web UI off for an empty saved address", () => {
    expect(restartPlan(page("http://192.168.0.99:8080"), "0.0.0.0:8080", "")).toEqual({ kind: "off", url: "http://192.168.0.99:8080" })
  })

  it.each([
    ["a wildcard keeps the page's hostname", "http://ghr.lan:8080", "0.0.0.0:8080", "0.0.0.0:9090", "http://ghr.lan:9090"],
    ["an IPv6 wildcard keeps it", "http://ghr.lan:8080", "[::]:8080", "[::]:9090", "http://ghr.lan:9090"],
    ["an expanded IPv6 wildcard keeps it", "http://ghr.lan:8080", "0.0.0.0:8080", "[0:0:0:0:0:0:0:0]:9090", "http://ghr.lan:9090"],
    ["a shortened IPv6 wildcard keeps it", "http://ghr.lan:8080", "0.0.0.0:8080", "[0::0]:9090", "http://ghr.lan:9090"],
    ["an IPv4 wildcard keeps it", "http://ghr.lan:8080", "[::]:8080", "0.0.0.0:9090", "http://ghr.lan:9090"],
    ["an empty host keeps it", "http://ghr.lan:8080", ":8080", ":9090", "http://ghr.lan:9090"],
    ["localhost keeps it", "http://localhost:8080", "localhost:8080", "localhost:9090", "http://localhost:9090"],
    ["a specific IP replaces it", "http://ghr.lan:8080", "0.0.0.0:8080", "192.168.0.99:9090", "http://192.168.0.99:9090"],
    ["a specific IPv6 address replaces it", "http://ghr.lan:8080", "0.0.0.0:8080", "[fd00::5]:9090", "http://[fd00::5]:9090"],
    ["the scheme's default port is left out", "http://ghr.lan", "0.0.0.0:80", "192.168.0.99:80", "http://192.168.0.99"],
  ])("moves a page that reached ghr directly: %s", (_name, origin, running, saved, url) => {
    expect(restartPlan(page(origin), running, saved)).toEqual({ kind: "moved", url })
  })

  it("names the new port for a proxy in front, and keeps the page's origin", () => {
    expect(restartPlan(page("https://ghr.example.com"), "127.0.0.1:8080", "127.0.0.1:9090")).toEqual({
      kind: "proxy",
      url: "https://ghr.example.com",
      port: "9090",
    })
  })
})
