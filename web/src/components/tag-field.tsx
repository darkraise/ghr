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
}: {
  label: string
  value: string[]
  onChange: (next: string[]) => void
  disabled?: boolean
  maxLength?: number
  className?: string
}) {
  const [text, setText] = useState("")
  const [tooLong, setTooLong] = useState(false)
  const fieldAria = useFieldAria()
  const errorId = useId()

  function add() {
    const tag = text.trim()
    if ([...tag].length > maxLength) {
      setTooLong(true)
      return
    }
    if (tag !== "" && !value.includes(tag)) onChange([...value, tag])
    setText("")
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
          aria-describedby={[fieldAria["aria-describedby"], tooLong ? errorId : ""].filter(Boolean).join(" ") || undefined}
          aria-invalid={fieldAria["aria-invalid"] || tooLong || undefined}
          aria-label={label}
          placeholder="+ add"
          className="min-w-0 flex-1"
          value={text}
          disabled={disabled}
          onChange={(e) => {
            setText(e.target.value)
            setTooLong(false)
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
      {tooLong && (
        <p id={errorId} role="alert" className="text-xs text-destructive">
          At most {maxLength} characters
        </p>
      )}
    </div>
  )
}
