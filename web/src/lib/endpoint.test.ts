import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { describe, expect, it } from "vitest"
import { endpointError } from "./endpoint"

interface Vector {
  endpoint: string
  error: string
}

const vectors = JSON.parse(readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "../../../internal/endpoint/testdata/vectors.json"), "utf8")) as Vector[]

describe("endpointError", () => {
  it("reads the vectors the daemon is tested with", () => {
    expect(vectors.length).toBeGreaterThan(0)
  })

  it.each(vectors.map((v): [string, string] => [v.endpoint, v.error]))("judges %j as the daemon does", (endpoint, error) => {
    expect(endpointError(endpoint)).toBe(error === "" ? null : error)
  })
})
