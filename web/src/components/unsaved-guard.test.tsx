import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router"
import { act, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { useEffect, useState } from "react"
import { describe, expect, it, vi } from "vitest"
import { UnsavedGuard } from "./unsaved-guard"

function setup(saveResult = true, settles = saveResult, allow?: (pathname: string) => boolean, hold?: Promise<void>) {
  const onSave = vi.fn()
  const onDiscard = vi.fn()
  const form: { edit: (count: number) => void } = { edit: () => {} }
  function Form() {
    const [count, setCount] = useState(1)
    useEffect(() => {
      form.edit = setCount
    }, [])
    return (
      <>
        <p>form page</p>
        <UnsavedGuard
          count={count}
          page="Settings"
          saving={false}
          allow={allow}
          onSave={async () => {
            onSave()
            await hold
            if (settles) setCount(0)
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
  const formRoute = createRoute({ getParentRoute: () => root, path: "/", component: Form })
  const other = createRoute({ getParentRoute: () => root, path: "/other", component: () => <p>other page</p> })
  const inside = createRoute({ getParentRoute: () => root, path: "/inside", component: () => <p>inside page</p> })
  const router = createRouter({ routeTree: root.addChildren([formRoute, other, inside]), history: createMemoryHistory({ initialEntries: ["/"] }) })
  render(<RouterProvider router={router} />)
  return { router, onSave, onDiscard, edit: (count: number) => form.edit(count), user: userEvent.setup() }
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

  it("stays when changes remain after the save", async () => {
    const { router, user, onSave } = setup(true, false)
    await leave(router)
    await user.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(screen.getByText("form page")).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })

  it("ignores a save that finishes after its prompt was dismissed", async () => {
    let finish: () => void = () => {}
    const hold = new Promise<void>((resolve) => (finish = resolve))
    const { router, user, edit } = setup(true, true, undefined, hold)
    await leave(router)
    await user.click(screen.getByRole("button", { name: "Save" }))
    await user.keyboard("{Escape}")
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    await act(async () => finish())
    act(() => edit(1))
    act(() => router.history.push("/other"))
    expect(await screen.findByText("You have 1 unsaved change on the Settings page.")).toBeInTheDocument()
    await expect(waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull(), { timeout: 300 })).rejects.toThrow()
    expect(router.state.location.pathname).toBe("/")
  })

  it("stays when the save fails", async () => {
    const { router, user } = setup(false)
    await leave(router)
    await user.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByText("form page")).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })

  it("lets a navigation within the same page through", async () => {
    const { router } = setup()
    await screen.findByText("form page")
    act(() => router.history.push("/#timing"))
    await waitFor(() => expect(router.state.location.hash).toBe("timing"))
    expect(screen.queryByText("You have 1 unsaved change on the Settings page.")).toBeNull()
    expect(router.state.location.pathname).toBe("/")
  })

  it("lets a navigation the page allows through", async () => {
    const { router } = setup(true, true, (pathname) => pathname === "/inside")
    await screen.findByText("form page")
    act(() => router.history.push("/inside"))
    expect(await screen.findByText("inside page")).toBeInTheDocument()
    expect(screen.queryByRole("alertdialog")).toBeNull()
  })

  it("still guards a navigation the page does not allow", async () => {
    const { router } = setup(true, true, (pathname) => pathname === "/inside")
    await leave(router)
    expect(router.state.location.pathname).toBe("/")
  })
})
