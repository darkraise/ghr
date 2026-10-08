import { ToggleGroup, ToggleGroupItem } from "darkraise-ui/components/toggle-group"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { useTheme } from "darkraise-ui/theme"
import { Monitor, Moon, Sun } from "lucide-react"

const MODES = [
  { value: "dark", label: "Dark", Icon: Moon },
  { value: "light", label: "Light", Icon: Sun },
  { value: "system", label: "System", Icon: Monitor },
] as const

export function ModeControl() {
  const { mode, setMode } = useTheme()
  return (
    <ToggleGroup
      type="single"
      size="sm"
      variant="outline"
      value={mode}
      aria-label="Colour mode"
      className="ghr-mode"
      onValueChange={(value) => {
        const next = MODES.find((m) => m.value === value)
        if (next) setMode(next.value)
      }}
    >
      {MODES.map(({ value, label, Icon }) => (
        <Tooltip key={value}>
          <TooltipTrigger asChild>
            <ToggleGroupItem value={value} aria-label={label}>
              <Icon size={15} aria-hidden="true" />
            </ToggleGroupItem>
          </TooltipTrigger>
          <TooltipContent>{label}</TooltipContent>
        </Tooltip>
      ))}
    </ToggleGroup>
  )
}
