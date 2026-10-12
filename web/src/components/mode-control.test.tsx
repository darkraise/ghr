import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { TooltipProvider } from "darkraise-ui/components/tooltip"
import { ThemeProvider } from "darkraise-ui/theme"
import { beforeEach, describe, expect, it } from "vitest"
import { themeConfig } from "@/theme.config"
import { ModeControl } from "./mode-control"

function draw() {
  const user = userEvent.setup()
  render(
    <ThemeProvider config={themeConfig}>
      <TooltipProvider>
        <ModeControl />
      </TooltipProvider>
    </ThemeProvider>,
  )
  return user
}

describe("ModeControl", () => {
  beforeEach(() => localStorage.clear())

  it("shows the system's mode, then toggles between light and dark", async () => {
    const user = draw()
    const toggle = screen.getByRole("button", { name: "Dark mode" })
    expect(toggle).toHaveAttribute("aria-pressed", "false")
    expect(localStorage.getItem("mode")).toBeNull()
    await user.click(toggle)
    expect(toggle).toHaveAttribute("aria-pressed", "true")
    expect(localStorage.getItem("mode")).toBe("dark")
    expect(document.documentElement.getAttribute("data-mode")).toBe("dark")
    await user.click(toggle)
    expect(toggle).toHaveAttribute("aria-pressed", "false")
    expect(localStorage.getItem("mode")).toBe("light")
    expect(document.documentElement.getAttribute("data-mode")).toBe("light")
  })

  it("names the mode in use and the one a click switches to", async () => {
    const user = draw()
    await user.hover(screen.getByRole("button", { name: "Dark mode" }))
    expect(await screen.findByRole("tooltip", {}, { timeout: 3000 })).toHaveTextContent("Light mode. Switch to dark")
  })
})
