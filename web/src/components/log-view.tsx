import { useEffect, useRef } from "react"

const NEAR_END = 8

export function LogView({
  text,
  follow,
  onFollowChange,
  className = "h-[60vh]",
}: {
  text: string
  follow: boolean
  onFollowChange?: (follow: boolean) => void
  className?: string
}) {
  const ref = useRef<HTMLPreElement>(null)
  useEffect(() => {
    const el = ref.current
    if (follow && el) el.scrollTop = el.scrollHeight
  }, [text, follow])

  // Scrolling away from the end pauses Follow, and scrolling back resumes it,
  // so reading earlier output is not yanked down by the next poll.
  function onScroll() {
    const el = ref.current
    if (!el || !onFollowChange) return
    const atEnd = el.scrollHeight - el.scrollTop - el.clientHeight <= NEAR_END
    if (atEnd !== follow) onFollowChange(atEnd)
  }

  return (
    <pre
      ref={ref}
      onScroll={onScroll}
      className={`${className} overflow-auto whitespace-pre-wrap rounded-md border bg-muted/40 p-3 font-mono text-xs`}
    >
      {text || "no log output yet"}
    </pre>
  )
}
