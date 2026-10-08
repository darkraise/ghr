import { useEffect, useRef, useState, type RefObject } from "react"

// Below this the chart scrolls inside its panel instead of squeezing, so the
// page body never scrolls sideways.
const MIN_WIDTH = 720

export function useWidth<T extends HTMLElement>(fallback: number): [RefObject<T | null>, number] {
  const ref = useRef<T>(null)
  const [width, setWidth] = useState(fallback)
  useEffect(() => {
    const el = ref.current
    if (!el || typeof ResizeObserver === "undefined") return
    const observer = new ResizeObserver(([entry]) => {
      if (entry) setWidth(Math.max(MIN_WIDTH, Math.floor(entry.contentRect.width)))
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [])
  return [ref, width]
}
