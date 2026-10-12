// The daemon's checks on web.listen and web.hosts (internal/config), so the
// form can refuse a value before sending it.

const HOSTNAME = /^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$/

// Go's net.SplitHostPort then strconv.Atoi, as config.Validate runs them on
// web.listen. The host comes back without brackets, the port as its number.
export function splitListen(listen: string): { host: string; port: string } | null {
  const colon = listen.lastIndexOf(":")
  if (colon < 0) return null
  let host: string
  let afterOpen = 0
  let afterClose = 0
  if (listen.startsWith("[")) {
    const close = listen.indexOf("]")
    if (close < 0 || close + 1 !== colon) return null
    host = listen.slice(1, close)
    afterOpen = 1
    afterClose = close + 1
  } else {
    host = listen.slice(0, colon)
    if (host.includes(":")) return null
  }
  if (listen.slice(afterOpen).includes("[") || listen.slice(afterClose).includes("]")) return null
  const port = listen.slice(colon + 1)
  if (!/^[+-]?\d+$/.test(port)) return null
  const n = Number(port)
  return n >= 1 && n <= 65535 ? { host, port: String(n) } : null
}

export function isHostname(host: string): boolean {
  return host.length <= 253 && HOSTNAME.test(host)
}
