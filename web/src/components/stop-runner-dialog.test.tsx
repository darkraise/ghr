import { render, screen, waitFor } from "@testing-library/react"
import type { ButtonHTMLAttributes, ReactNode } from "react"
import { describe, expect, it, vi } from "vitest"
import { keys } from "@/api/hooks"
import type { InstanceStatus, Status } from "@/api/types"
import { mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { StopRunnerDialog } from "./stop-runner-dialog"

// The kit keeps a closing dialog's content mounted while its exit animation
// runs. jsdom runs no animation, so this stand-in renders the content whether
// the dialog is open or not.
vi.mock("darkraise-ui/components/alert-dialog", () => {
  const Box = ({ children }: { children?: ReactNode }) => <div>{children}</div>
  const Btn = ({ children, ...rest }: ButtonHTMLAttributes<HTMLButtonElement>) => <button {...rest}>{children}</button>
  return {
    AlertDialog: Box,
    AlertDialogContent: Box,
    AlertDialogHeader: Box,
    AlertDialogFooter: Box,
    AlertDialogTitle: ({ children }: { children?: ReactNode }) => <h2>{children}</h2>,
    AlertDialogDescription: ({ children }: { children?: ReactNode }) => <p>{children}</p>,
    AlertDialogCancel: Btn,
    AlertDialogAction: Btn,
  }
})

const idle = fixtures.status.instances.find((i) => i.id === "bbbbbb") as InstanceStatus
const nowStopping: Status = {
  ...fixtures.status,
  instances: fixtures.status.instances.map((i) => (i.id === "bbbbbb" ? { ...i, stopping: true } : i)),
}

describe("StopRunnerDialog", () => {
  it("keeps its wording while it closes, and says already stopping while open", async () => {
    let stopping = false
    mockApi(authedRoutes({ "GET /api/status": () => (stopping ? nowStopping : fixtures.status) }))
    const { queryClient, wrapper } = withQuery()
    const { rerender } = render(<StopRunnerDialog instance={idle} onClose={() => {}} />, { wrapper })
    expect(screen.getByRole("heading")).toHaveTextContent("Stop runner bbbbbb?")
    await waitFor(() => expect(queryClient.getQueryData(keys.status)).toEqual(fixtures.status))

    // Closing: the runner turns up as stopping, and the wording must not move.
    rerender(<StopRunnerDialog instance={null} onClose={() => {}} />)
    stopping = true
    await queryClient.invalidateQueries({ queryKey: keys.status })
    await waitFor(() => expect(queryClient.getQueryData(keys.status)).toEqual(nowStopping))
    expect(screen.getByRole("heading")).toHaveTextContent("Stop runner bbbbbb?")
    expect(screen.getByText("ghr stops the runner and cleans it up.")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Stop" })).toBeEnabled()

    // Open over the same runner: now it says so and offers no second stop.
    rerender(<StopRunnerDialog instance={idle} onClose={() => {}} />)
    expect(screen.getByRole("heading")).toHaveTextContent("Runner bbbbbb is already stopping")
    expect(screen.getByText("ghr is stopping it and will clean it up.")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Stop" })).toBeDisabled()
  })
})
