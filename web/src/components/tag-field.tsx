import { Badge } from "darkraise-ui/components/badge"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { useState } from "react"

const MAX_TAG = 100

export function TagField({
  label,
  value,
  onChange,
  disabled,
}: {
  label: string
  value: string[]
  onChange: (next: string[]) => void
  disabled?: boolean
}) {
  const [text, setText] = useState("")

  function add() {
    const tag = text.trim()
    if (tag !== "" && tag.length <= MAX_TAG && !value.includes(tag)) onChange([...value, tag])
    setText("")
  }

  return (
    <div className="flex flex-col gap-2">
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
          aria-label={label}
          placeholder="+ add"
          value={text}
          disabled={disabled}
          onChange={(e) => setText(e.target.value)}
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
    </div>
  )
}
