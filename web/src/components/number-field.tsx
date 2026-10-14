import {
  NumberInput,
  NumberInputControl,
  NumberInputDecrementTrigger,
  NumberInputField,
  NumberInputIncrementTrigger,
  NumberInputSuffix,
  NumberInputTriggerGroup,
} from "darkraise-ui/components/number-input"
import type { KeyboardEvent, PointerEvent } from "react"

interface NumberFieldProps {
  id?: string
  // Names the field and its steppers, which would otherwise all be "Increment"
  // and "Decrement".
  label: string
  // NaN is an empty box. undefined would hand the value over to NumberInput,
  // which then stops following this prop.
  value: number
  onValueChange: (value: number) => void
  // The value an empty box stands for, which stepping starts from. Defaults to min.
  emptyValue?: number
  min?: number
  max?: number
  step?: number
  precision?: number
  suffix?: string
  // Read in place of suffix where the shown unit reads badly aloud, such as "s".
  spokenSuffix?: string
  placeholder?: string
  disabled?: boolean
  className?: string
  "aria-invalid"?: boolean
  "aria-describedby"?: string
}

const KEY_STEPS: Record<string, number> = { ArrowUp: 1, ArrowDown: -1, PageUp: 10, PageDown: -10 }

// NumberInput reports each number as it is typed and clamps to min and max
// only when the field loses focus. It rounds to precision only then too, so a
// whole-number field truncates what it reports while the typed text stays.
//
// From an empty box NumberInput steps up from min but down from max, while an
// empty box here stands for an inherited or default value. So NumberField
// steps an empty box itself, from emptyValue; NumberInput skips a key or press
// whose default is prevented.
export function NumberField({
  id,
  label,
  value,
  onValueChange,
  emptyValue,
  min,
  max,
  step = 1,
  precision,
  suffix,
  spokenSuffix,
  placeholder,
  disabled,
  className,
  ...aria
}: NumberFieldProps) {
  const unit = spokenSuffix ?? suffix
  const report = (n: number) => onValueChange(precision === 0 && Number.isFinite(n) ? Math.trunc(n) : n)

  function stepEmpty(event: KeyboardEvent | PointerEvent, steps: number) {
    if (!Number.isNaN(value) || disabled) return
    event.preventDefault()
    const next = (emptyValue ?? min ?? 0) + steps * step
    report(Math.min(max ?? Infinity, Math.max(min ?? -Infinity, next)))
  }

  function onKeyDown(event: KeyboardEvent) {
    const steps = KEY_STEPS[event.key]
    if (steps === undefined) return
    stepEmpty(event, event.shiftKey && Math.abs(steps) === 1 ? steps * 10 : steps)
  }

  return (
    <NumberInput
      className={className}
      value={value}
      onValueChange={(d) => report(d.valueAsNumber)}
      min={min}
      max={max}
      step={step}
      precision={precision}
      disabled={disabled}
    >
      <NumberInputControl>
        <NumberInputField
          id={id}
          aria-label={label}
          aria-valuetext={unit !== undefined && !Number.isNaN(value) ? `${value} ${unit}` : undefined}
          placeholder={placeholder}
          onKeyDown={onKeyDown}
          {...aria}
        />
        {suffix !== undefined && <NumberInputSuffix>{suffix}</NumberInputSuffix>}
        <NumberInputTriggerGroup>
          <NumberInputIncrementTrigger aria-label={`Increase ${label}`} onPointerDown={(e) => stepEmpty(e, 1)} />
          <NumberInputDecrementTrigger aria-label={`Decrease ${label}`} onPointerDown={(e) => stepEmpty(e, -1)} />
        </NumberInputTriggerGroup>
      </NumberInputControl>
    </NumberInput>
  )
}
