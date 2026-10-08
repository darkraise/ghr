import { useState } from "react"
import type { ActivityWindow } from "@/api/types"
import { ACTIVITY_WINDOWS } from "./activity-view"

const KEY = "ghr-activity-window"

function stored(): ActivityWindow {
  try {
    const saved = localStorage.getItem(KEY)
    return ACTIVITY_WINDOWS.find((w) => w === saved) ?? "1h"
  } catch {
    return "1h"
  }
}

export function useActivityWindow(): [ActivityWindow, (w: ActivityWindow) => void] {
  const [current, setCurrent] = useState<ActivityWindow>(stored)
  function choose(w: ActivityWindow) {
    setCurrent(w)
    try {
      localStorage.setItem(KEY, w)
    } catch {
      // storage refused, as in a private window: the choice lasts this visit
    }
  }
  return [current, choose]
}
