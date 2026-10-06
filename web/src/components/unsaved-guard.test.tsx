import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router"
import { act, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { useState } from "react"
import { describe, expect, it, vi } from "vitest"
import { UnsavedGuard } from "./unsaved-guard"

function setup(saveResult = true) {
  const onSave = vi.fn()
  const onDiscard = vi.fn()
  function Form() {
    const [count, setCount] = useState(1)
    return (
      <>
        <p>form page</p>
        <UnsavedGuard
          count={count}
          page="Settings"
          saving={false}
          onSave={async () => {
            onSave()
            if (saveResult) setCount(0)
            return saveResult
          }}
          onDiscard={() => {
            onDiscard()
            setCount(0)
          }}
        />
      </>
    )
  }
  const root = createRootRoute({ component: () => <Outlet /> })
  const form = createRoute({ getParentRoute: () => root, path: "/", component: Form })
  const other = createRoute({ getParentRoute: () => root, path: "/other", component: () => <p>other page</p> })
  const router = createRouter({ routeTree: root.addChildren([form, other]), history: createMemoryHistory({ initialEntries: ["/"] }) })
  render(<RouterProvider router={router} />)
  return { router, onSave, onDiscard, user: userEvent.setup() }
}

async function leave(router: ReturnType<typeof setup>["router"]) {
  await screen.findByText("form page")
  act(() => router.history.push("/other"))
  expect(await screen.findByText("You have 1 unsaved change on the Settings page.")).toBeInTheDocument()
}

describe("UnsavedGuard", () => {
  it("stays on Stay", async () => {
    const { router, user } = setup()
    await leave(router)
    await user.click(screen.getByRole("button", { name: "Stay" }))
    expect(screen.getByText("form page")).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })

  it("drops the edits and leaves on Discard", async () => {
    const { router, user, onDiscard } = setup()
    await leave(router)
    await user.click(screen.getByRole("button", { name: "Discard" }))
    expect(await screen.findByText("other page")).toBeInTheDocument()
    expect(onDiscard).toHaveBeenCalledTimes(1)
  })

  it("saves, then leaves", async () => {
    const { router, user, onSave } = setup()
    await leave(router)
    await user.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByText("other page")).toBeInTheDocument()
    expect(onSave).toHaveBeenCalledTimes(1)
  })

  it("stays when the save fails", async () => {
    const { router, user } = setup(false)
    await leave(router)
    await user.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByText("form page")).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })
})
