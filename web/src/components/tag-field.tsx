import { Badge } from "darkraise-ui/components/badge"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { useId, useState } from "react"
import { useFieldAria } from "@/components/page/field-aria"

const MAX_TAG = 100

export function TagField({
  label,
  value,
  onChange,
  disabled,
  maxLength = MAX_TAG,
  className = "",
  exact,
}: {
  label: string
  value: string[]
  onChange: (next: string[]) => void
  disabled?: boolean
  maxLength?: number
  className?: string
  // The daemon refuses labels that differ only in case, so a duplicate is
  // found ignoring case unless the list is one it compares exactly.
  exact?: boolean
}) {
  const [text, setText] = useState("")
  const [refused, setRefused] = useState("")
  const fieldAria = useFieldAria()
  const errorId = useId()

  function add() {
    const tag = text.trim()
    if ([...tag].length > maxLength) return setRefused(`At most ${maxLength} characters`)
    const listed = value.find((t) => (exact ? t === tag : t.toLowerCase() === tag.toLowerCase()))
    if (listed !== undefined) return setRefused(`Already listed as ${listed}`)
    if (tag !== "") onChange([...value, tag])
    setText("")
    setRefused("")
  }

  return (
    <div className={`flex flex-col gap-2 ${className}`}>
      {value.length > 0 && (
        <ul className="flex flex-wrap gap-1">
          {value.map((tag) => (
            <li key={tag}>
              <Badge variant="secondary" className="gap-1">
                {tag}
                <button
                  type="button"
                  aria-label={`Remove ${tag}`}
                  disabled={disabled}
                  onClick={() => onChange(value.filter((t) => t !== tag))}
                >
                  ✕
                </button>
              </Badge>
            </li>
          ))}
        </ul>
      )}
      <div className="flex gap-2">
        <Input
          {...fieldAria}
          aria-describedby={[fieldAria["aria-describedby"], refused ? errorId : ""].filter(Boolean).join(" ") || undefined}
          aria-invalid={fieldAria["aria-invalid"] || Boolean(refused) || undefined}
          aria-label={label}
          placeholder="+ add"
          className="min-w-0 flex-1"
          value={text}
          disabled={disabled}
          onChange={(e) => {
            setText(e.target.value)
            setRefused("")
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault()
              add()
            }
          }}
        />
        <Button type="button" variant="outline" aria-label={`Add to ${label}`} disabled={disabled || text.trim() === ""} onClick={add}>
          Add
        </Button>
      </div>
      {refused && (
        <p id={errorId} role="alert" className="text-xs text-destructive">
          {refused}
        </p>
      )}
    </div>
  )
}
