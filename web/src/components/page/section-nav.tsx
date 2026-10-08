import { useEffect, useState } from "react"
import { useMediaQuery, WIDEST } from "@/lib/use-media-query"

export interface NavItem {
  id: string
  title: string
}

// A section sits under the page header once its top passes this line.
const CURRENT_LINE = 96

// Buttons, not hash links: a hash change is a navigation, and UnsavedGuard
// would treat it as leaving the page. items must be a stable array.
export function SectionNav({ items }: { items: readonly NavItem[] }) {
  const wide = useMediaQuery(WIDEST)
  const [current, setCurrent] = useState(items[0]?.id)

  useEffect(() => {
    function onScroll() {
      let found = items[0]?.id
      for (const item of items) {
        const el = document.getElementById(item.id)
        if (el && el.getBoundingClientRect().top <= CURRENT_LINE) found = item.id
      }
      setCurrent(found)
    }
    // A page reloaded already scrolled fires no scroll event.
    onScroll()
    // Capture catches the scroll of whichever element holds the page.
    document.addEventListener("scroll", onScroll, { capture: true, passive: true })
    return () => document.removeEventListener("scroll", onScroll, { capture: true })
  }, [items])

  if (!wide) return null

  function jump(id: string) {
    document.getElementById(id)?.scrollIntoView({ block: "start" })
    document.getElementById(`${id}-title`)?.focus({ preventScroll: true })
    setCurrent(id)
  }

  return (
    <nav aria-label="Sections" className="sticky top-4 self-start">
      <ul className="flex flex-col gap-0.5 border-l border-border text-sm">
        {items.map((item) => (
          <li key={item.id}>
            <button
              type="button"
              aria-current={item.id === current ? "true" : undefined}
              onClick={() => jump(item.id)}
              className={`-ml-px w-full border-l-2 px-3 py-1 text-left ${
                item.id === current ? "border-primary font-medium text-foreground" : "border-transparent text-muted-foreground hover:text-foreground"
              }`}
            >
              {item.title}
            </button>
          </li>
        ))}
      </ul>
    </nav>
  )
}
