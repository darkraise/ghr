import { ToggleGroup, ToggleGroupItem } from "darkraise-ui/components/toggle-group"
import type { ActivityWindow } from "@/api/types"
import { ACTIVITY_WINDOWS } from "@/lib/activity-view"

export function WindowControl({ value, onChange }: { value: ActivityWindow; onChange: (w: ActivityWindow) => void }) {
  return (
    <ToggleGroup
      type="single"
      size="sm"
      variant="outline"
      value={value}
      aria-label="Time window"
      onValueChange={(next) => {
        const w = ACTIVITY_WINDOWS.find((x) => x === next)
        if (w) onChange(w)
      }}
    >
      {ACTIVITY_WINDOWS.map((w) => (
        <ToggleGroupItem key={w} value={w} className="font-mono">
          {w}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}
