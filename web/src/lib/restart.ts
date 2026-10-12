import { splitListen } from "@/lib/listen"

export interface Page {
  protocol: string
  hostname: string
  port: string
  origin: string
}

export interface RestartPlan {
  kind: "same" | "moved" | "proxy" | "off"
  url: string
  port?: string
}

const IPV4 = /^\d{1,3}(\.\d{1,3}){3}$/

// An empty host, 0.0.0.0, or an IPv6 address whose groups are all zero in
// any spelling (::, 0::0, 0:0:0:0:0:0:0:0) listens on every address.
function unspecified(host: string): boolean {
  if (host === "") return true
  if (IPV4.test(host)) return host.split(".").every((part) => Number(part) === 0)
  return host.includes(":") && host.split(":").every((group) => /^0{0,4}$/.test(group))
}

function specificIP(host: string): boolean {
  return !unspecified(host) && (IPV4.test(host) || host.includes(":"))
}

// Where the web UI answers after a restart. running is the listen value the
// running listener serves; saved is the one the restart applies, equal to
// running when web.listen is not pending. A page whose port is the running
// listener's reached ghr directly; any other went through a proxy.
export function restartPlan(page: Page, running: string | undefined, saved: string | undefined): RestartPlan {
  if (saved === running) return { kind: "same", url: page.origin }
  const value = saved?.trim() ?? ""
  if (value === "") return { kind: "off", url: page.origin }
  const next = splitListen(value)
  // The daemon refuses an address it cannot parse, so none is pending.
  if (!next) return { kind: "same", url: page.origin }
  const pagePort = page.port || (page.protocol === "https:" ? "443" : "80")
  const now = running ? splitListen(running) : null
  if (now?.port !== pagePort) return { kind: "proxy", url: page.origin, port: next.port }
  const url = new URL(page.origin)
  if (specificIP(next.host)) url.hostname = next.host.includes(":") ? `[${next.host}]` : next.host
  url.port = next.port
  return { kind: "moved", url: url.origin }
}
