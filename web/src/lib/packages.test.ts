import { describe, expect, it } from "vitest"
import type { PackageInfo } from "@/api/types"
import { filterPackages, upgradable, validTerm } from "./packages"

const pkg = (name: string, description?: string): PackageInfo => ({ name, description, installed: true, bytes: 1 })

describe("validTerm", () => {
  it.each([
    ["rip", true],
    ["g++", true],
    ["libc6.1-dev", true],
    ["a".repeat(64), true],
    ["r", false],
    ["", false],
    ["Rip", false],
    ["rip grep", false],
    ["jq;rm", false],
    ["a".repeat(65), false],
  ])("reads %j as %s", (term, want) => {
    expect(validTerm(term)).toBe(want)
  })
})

describe("upgradable", () => {
  it("lists the managed packages with a newer candidate", () => {
    const managed = [{ ...pkg("ripgrep"), candidate: "14.1.1-1", upgradable: true }, pkg("shellcheck"), { ...pkg("yq"), installed: false }]
    expect(upgradable({ managed, base: [], detected: [] }).map((p) => p.name)).toEqual(["ripgrep"])
  })

  it("is empty without a list", () => {
    expect(upgradable(undefined)).toEqual([])
    expect(upgradable({ managed: null, base: null, detected: null })).toEqual([])
  })
})

describe("filterPackages", () => {
  const list = [pkg("vim", "Vi IMproved"), pkg("zstd", "fast lossless compression"), pkg("vim-tiny")]

  it.each([
    ["", ["vim", "zstd", "vim-tiny"]],
    ["  ", ["vim", "zstd", "vim-tiny"]],
    ["vim", ["vim", "vim-tiny"]],
    ["VIM", ["vim", "vim-tiny"]],
    ["lossless", ["zstd"]],
    ["nothing", []],
  ])("keeps the rows matching %j by name or description", (filter, want) => {
    expect(filterPackages(list, filter).map((p) => p.name)).toEqual(want)
  })

  it("reads a missing list as empty", () => {
    expect(filterPackages(null, "vim")).toEqual([])
  })
})
