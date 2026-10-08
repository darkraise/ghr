import { ToggleGroup, ToggleGroupItem } from "darkraise-ui/components/toggle-group"
import type { ActivityWindow } from "@/api/types"
import { ACTIVITY_WINDOWS } from "@/lib/activity-view"

export function WindowControl<W extends ActivityWindow = ActivityWindow>({
  value,
  onChange,
  options = ACTIVITY_WINDOWS as readonly W[],
}: {
  value: W
  onChange: (w: W) => void
  options?: readonly W[]
}) {
  return (
    <ToggleGroup
      type="single"
      size="sm"
      variant="outline"
      value={value}
      aria-label="Time window"
      onValueChange={(next) => {
        const w = options.find((x) => x === next)
        if (w) onChange(w)
      }}
    >
      {options.map((w) => (
        <ToggleGroupItem key={w} value={w} className="font-mono">
          {w}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}
