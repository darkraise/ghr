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

  it("starts on dark and saves the chosen mode", async () => {
    const user = draw()
    expect(screen.getByRole("radiogroup", { name: "Colour mode" })).toBeInTheDocument()
    expect(screen.getByRole("radio", { name: "Dark" })).toBeChecked()
    await user.click(screen.getByRole("radio", { name: "Light" }))
    expect(screen.getByRole("radio", { name: "Light" })).toBeChecked()
    expect(localStorage.getItem("mode")).toBe("light")
    expect(document.documentElement.getAttribute("data-mode")).toBe("light")
  })

  it("names each mode in a tooltip", async () => {
    const user = draw()
    await user.hover(screen.getByRole("radio", { name: "System" }))
    expect(await screen.findByRole("tooltip", {}, { timeout: 3000 })).toHaveTextContent("System")
  })
})
