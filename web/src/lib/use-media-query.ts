import { useSyncExternalStore } from "react"

export const WIDE = "(min-width: 1024px)"
export const WIDEST = "(min-width: 1280px)"

export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    (onChange) => {
      const media = window.matchMedia(query)
      media.addEventListener("change", onChange)
      return () => media.removeEventListener("change", onChange)
    },
    () => window.matchMedia(query).matches,
    () => false,
  )
}
