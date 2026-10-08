import { useCallback, useSyncExternalStore } from "react"

export const WIDE = "(min-width: 1024px)"
export const WIDEST = "(min-width: 1280px)"

export function useMediaQuery(query: string): boolean {
  // A stable subscribe keeps the listener in place across renders.
  const subscribe = useCallback(
    (onChange: () => void) => {
      const media = window.matchMedia(query)
      media.addEventListener("change", onChange)
      return () => media.removeEventListener("change", onChange)
    },
    [query],
  )
  return useSyncExternalStore(subscribe, () => window.matchMedia(query).matches, () => false)
}
