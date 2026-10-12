import { describe, expect, it } from "vitest"
import { httpsUrl } from "./links"

describe("httpsUrl", () => {
  it("passes an https address through", () => {
    expect(httpsUrl("https://github.com/darkraise/darkmem/actions/runs/101")).toBe("https://github.com/darkraise/darkmem/actions/runs/101")
  })

  it.each([["http://github.com/darkraise/darkmem"], ["javascript:alert(1)"], ["//github.com/darkraise"], ["/actions"], [""], [undefined]])(
    "gives no address for %s",
    (url) => {
      expect(httpsUrl(url)).toBeUndefined()
    },
  )
})
