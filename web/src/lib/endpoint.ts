// Mirrors Go's endpoint.Check, including the parts of net/url and net/netip it
// relies on; both are tested with internal/endpoint/testdata/vectors.json. The
// browser's URL parser is no use here: it rewrites 010.0.0.1 and 10.1 as IPv4
// addresses and refuses zones, where Go refuses them or keeps them.

const MSG_FORM = "the endpoint must be https://host[:port], or http://ip[:port] for a private, loopback or link-local IP address"
const MSG_EXTRA = "the endpoint must be only https://host[:port] or http://ip[:port], without a path, query or user info"
const MSG_ZONE = "an IPv6 zone is not supported"
const MSG_HTTP_NAME = "use https:// for a host name; http:// is accepted only for a private, loopback or link-local IP address"
const MSG_HTTP_ADDR = "http:// is accepted only for a private, loopback or link-local IP address; use https:// for this address"

type Mode = "host" | "zone" | "other"

const HOST_BYTE = /^[A-Za-z0-9!$&'()*+,;=:[\]<>"\-_.~]$/
const USER_INFO = /^[A-Za-z0-9\-._:~!$&'()*+,;=%@]*$/
const PORT = /^(:[0-9]*)?$/

const hostByte = (c: number) => c < 0x80 && HOST_BYTE.test(String.fromCharCode(c))

function hexValue(c: number): number {
  if (c >= 0x30 && c <= 0x39) return c - 0x30
  if (c >= 0x61 && c <= 0x66) return c - 0x61 + 10
  if (c >= 0x41 && c <= 0x46) return c - 0x41 + 10
  return -1
}

// null where Go's unescape fails. A host may %-escape only non-ASCII bytes
// and %25; a zone only bytes a host may hold, a space and %25.
function unescape(s: string, mode: Mode): string | null {
  let out = ""
  for (let i = 0; i < s.length; ) {
    const c = s.charCodeAt(i)
    if (c === 0x25) {
      const hi = hexValue(s.charCodeAt(i + 1))
      const lo = hexValue(s.charCodeAt(i + 2))
      if (hi < 0 || lo < 0) return null
      const v = hi * 16 + lo
      if (mode === "host" && v < 0x80 && v !== 0x25) return null
      if (mode === "zone" && v !== 0x25 && v !== 0x20 && !hostByte(v)) return null
      out += String.fromCharCode(v)
      i += 3
      continue
    }
    if (mode !== "other" && c < 0x80 && !hostByte(c)) return null
    out += s.charAt(i)
    i++
  }
  return out
}

function parseIPv4(s: string): number[] | null {
  const fields: number[] = []
  let val = 0
  let digits = 0
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i)
    if (c >= 0x30 && c <= 0x39) {
      if (digits === 1 && val === 0) return null
      val = val * 10 + (c - 0x30)
      digits++
      if (val > 255) return null
    } else if (c === 0x2e) {
      if (i === 0 || i === s.length - 1 || s.charCodeAt(i - 1) === 0x2e) return null
      if (fields.length === 3) return null
      fields.push(val)
      val = 0
      digits = 0
    } else {
      return null
    }
  }
  if (fields.length < 3) return null
  fields.push(val)
  return fields
}

function parseIPv6(input: string): Uint8Array | null {
  let s = input
  const z = s.indexOf("%")
  if (z !== -1) {
    if (z === s.length - 1) return null
    s = s.slice(0, z)
  }
  const ip = new Uint8Array(16)
  let ellipsis = -1
  if (s.startsWith("::")) {
    ellipsis = 0
    s = s.slice(2)
    if (s === "") return ip
  }
  let i = 0
  while (i < 16) {
    let off = 0
    let acc = 0
    for (; off < s.length; off++) {
      const d = hexValue(s.charCodeAt(off))
      if (d < 0) break
      acc = acc * 16 + d
      if (off > 3) return null
    }
    if (off === 0) return null
    if (s.charAt(off) === ".") {
      if (ellipsis < 0 && i !== 12) return null
      if (i + 4 > 16) return null
      const v4 = parseIPv4(s)
      if (v4 === null) return null
      ip.set(v4, i)
      s = ""
      i += 4
      break
    }
    ip[i] = acc >> 8
    ip[i + 1] = acc & 0xff
    i += 2
    s = s.slice(off)
    if (s === "") break
    if (s.charAt(0) !== ":" || s.length === 1) return null
    s = s.slice(1)
    if (s.charAt(0) === ":") {
      if (ellipsis >= 0) return null
      ellipsis = i
      s = s.slice(1)
      if (s === "") break
    }
  }
  if (s !== "") return null
  if (i < 16) {
    if (ellipsis < 0) return null
    const n = 16 - i
    ip.copyWithin(ellipsis + n, ellipsis, i)
    ip.fill(0, ellipsis, ellipsis + n)
  } else if (ellipsis >= 0) {
    return null
  }
  return ip
}

// The address as 4 or 16 bytes, as Go's netip.ParseAddr reads it.
function parseAddr(s: string): Uint8Array | null {
  for (let i = 0; i < s.length; i++) {
    const c = s.charAt(i)
    if (c === ".") {
      const v4 = parseIPv4(s)
      return v4 === null ? null : Uint8Array.from(v4)
    }
    if (c === ":") return parseIPv6(s)
    if (c === "%") return null
  }
  return null
}

function allowedOverHTTP(addr: Uint8Array): boolean {
  let b = addr
  if (b.length === 16 && b.subarray(0, 10).every((x) => x === 0) && b[10] === 0xff && b[11] === 0xff) b = b.subarray(12)
  const at = (i: number) => b[i] ?? 0
  if (b.length === 4) {
    return at(0) === 10 || (at(0) === 172 && (at(1) & 0xf0) === 16) || (at(0) === 192 && at(1) === 168) || at(0) === 127 || (at(0) === 169 && at(1) === 254)
  }
  const loopback = b.every((x, i) => x === (i === 15 ? 1 : 0))
  return (at(0) & 0xfe) === 0xfc || (at(0) === 0xfe && (at(1) & 0xc0) === 0x80) || loopback
}

// Go's parseHost for http and https, which allow one colon outside brackets.
function parseHost(host: string): string | null {
  const open = host.lastIndexOf("[")
  if (open > 0) return null
  if (open === 0) {
    const close = host.lastIndexOf("]")
    if (close < 0) return null
    const port = host.slice(close + 1)
    if (!PORT.test(port)) return null
    const inner = host.slice(1, close)
    const zoneAt = inner.indexOf("%25")
    let name: string | null
    if (zoneAt < 0) {
      name = unescape(inner, "host")
    } else {
      const addr = unescape(inner.slice(0, zoneAt), "host")
      const zone = unescape(inner.slice(zoneAt), "zone")
      name = addr === null || zone === null ? null : addr + zone
    }
    if (name === null) return null
    const addr = parseAddr(name)
    if (addr === null || addr.length === 4) return null
    return `[${name}]${port}`
  }
  const colon = host.indexOf(":")
  if (colon !== -1 && !PORT.test(host.slice(colon))) return null
  return unescape(host, "host")
}

// Go's splitHostPort: the port counts only when it is all digits.
function splitHostPort(host: string): { name: string; port: string } {
  let name = host
  let port = ""
  const colon = name.lastIndexOf(":")
  if (colon !== -1 && PORT.test(name.slice(colon))) {
    port = name.slice(colon + 1)
    name = name.slice(0, colon)
  }
  if (name.startsWith("[") && name.endsWith("]")) name = name.slice(1, -1)
  return { name, port }
}

// null when ghr accepts raw as its S3 endpoint, otherwise the daemon's reason.
export function endpointError(raw: string): string | null {
  const hash = raw.indexOf("#")
  const head = hash < 0 ? raw : raw.slice(0, hash)
  const fragment = hash < 0 ? "" : raw.slice(hash + 1)
  // eslint-disable-next-line no-control-regex
  if (/[\x00-\x1f\x7f]/.test(head)) return MSG_FORM
  const scheme = /^([A-Za-z][A-Za-z0-9+.-]*):/.exec(head)
  const name = scheme?.[1]?.toLowerCase()
  if (!scheme || (name !== "http" && name !== "https")) return MSG_FORM
  let rest = head.slice(scheme[0].length)
  const q = rest.indexOf("?")
  const query = q >= 0
  if (query) rest = rest.slice(0, q)
  if (!rest.startsWith("//")) return MSG_FORM
  const slash = rest.indexOf("/", 2)
  const authority = slash < 0 ? rest.slice(2) : rest.slice(2, slash)
  const path = slash < 0 ? "" : rest.slice(slash)
  const at = authority.lastIndexOf("@")
  const host = parseHost(authority.slice(at + 1))
  if (host === null) return MSG_FORM
  const user = at >= 0
  if (user && (!USER_INFO.test(authority.slice(0, at)) || unescape(authority.slice(0, at), "other") === null)) return MSG_FORM
  if (unescape(path, "other") === null || unescape(fragment, "other") === null) return MSG_FORM
  const { name: h, port } = splitHostPort(host)
  if (h === "" || host.endsWith(":") || (port !== "" && !(Number(port) >= 1 && Number(port) <= 65535))) return MSG_FORM
  if (user || path !== "" || query || hash >= 0) return MSG_EXTRA
  if (h.includes("%")) return MSG_ZONE
  if (name === "https") return null
  const addr = parseAddr(h)
  if (addr === null) return MSG_HTTP_NAME
  return allowedOverHTTP(addr) ? null : MSG_HTTP_ADDR
}
