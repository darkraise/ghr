import { Button } from "darkraise-ui/components/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { useTheme } from "darkraise-ui/theme"
import { Moon, Sun } from "lucide-react"

// The icon shows the mode in use. Until the first click that is the system's
// mode; a click saves the other one.
export function ModeControl() {
  const { resolvedMode, setMode } = useTheme()
  const dark = resolvedMode === "dark"
  const Icon = dark ? Moon : Sun
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button size="icon" variant="ghost" aria-label="Dark mode" aria-pressed={dark} onClick={() => setMode(dark ? "light" : "dark")}>
          <Icon size={15} aria-hidden="true" />
        </Button>
      </TooltipTrigger>
      <TooltipContent>{dark ? "Dark mode. Switch to light" : "Light mode. Switch to dark"}</TooltipContent>
    </Tooltip>
  )
}
