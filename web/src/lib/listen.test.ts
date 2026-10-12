import { describe, expect, it } from "vitest"
import { isHostname, splitListen } from "./listen"

describe("splitListen", () => {
  it.each([
    ["0.0.0.0:8080", { host: "0.0.0.0", port: "8080" }],
    [":8080", { host: "", port: "8080" }],
    ["[::]:9090", { host: "::", port: "9090" }],
    ["[fd00::5]:80", { host: "fd00::5", port: "80" }],
    ["localhost:1", { host: "localhost", port: "1" }],
    ["192.168.0.99:65535", { host: "192.168.0.99", port: "65535" }],
  ])("reads %s", (listen, want) => {
    expect(splitListen(listen)).toEqual(want)
  })

  // strconv.Atoi takes a sign and leading zeros.
  it.each([
    ["0.0.0.0:000080", { host: "0.0.0.0", port: "80" }],
    ["0.0.0.0:+8080", { host: "0.0.0.0", port: "8080" }],
    ["[]:80", { host: "", port: "80" }],
    ["[fe80::1%eth0]:80", { host: "fe80::1%eth0", port: "80" }],
  ])("reads %s as Go does", (listen, want) => {
    expect(splitListen(listen)).toEqual(want)
  })

  it.each([
    "",
    "0.0.0.0",
    "0.0.0.0:",
    "0.0.0.0:0",
    "0.0.0.0:65536",
    "0.0.0.0:-80",
    "0.0.0.0: 80",
    "0.0.0.0:http",
    "0.0.0.0:99999999999999999999",
    "::1:80",
    "[::1]",
    "[::1]80",
    "[::1:8080",
    "127.0.0.1]:8080",
    "[::1]:80:90",
    "[::1]x:80",
    "[[::1]:80",
    "[::1]]:80",
    "a[b:80",
  ])("refuses %j", (listen) => {
    expect(splitListen(listen)).toBeNull()
  })
})

describe("isHostname", () => {
  it.each(["ghr.lan", "ghr", "GHR.example.com", "a-b.c"])("takes %s", (host) => {
    expect(isHostname(host)).toBe(true)
  })

  it.each(["https://ghr.lan", "ghr.lan:8080", "-ghr.lan", "ghr..lan", "ghr_lan", "", `${"a".repeat(63)}.${"b".repeat(63)}.${"c".repeat(63)}.${"d".repeat(63)}`])(
    "refuses %j",
    (host) => {
      expect(isHostname(host)).toBe(false)
    },
  )
})
