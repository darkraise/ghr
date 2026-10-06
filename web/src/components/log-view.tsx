import { useEffect, useRef } from "react"

export function LogView({ text, follow }: { text: string; follow: boolean }) {
  const ref = useRef<HTMLPreElement>(null)
  useEffect(() => {
    const el = ref.current
    if (follow && el) el.scrollTop = el.scrollHeight
  }, [text, follow])
  return (
    <pre ref={ref} className="h-[60vh] overflow-auto whitespace-pre-wrap rounded-md border bg-muted/40 p-3 font-mono text-xs">
      {text || "no log output yet"}
    </pre>
  )
}
