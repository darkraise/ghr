import {
  NumberInput,
  NumberInputControl,
  NumberInputDecrementTrigger,
  NumberInputField,
  NumberInputIncrementTrigger,
  NumberInputSuffix,
  NumberInputTriggerGroup,
} from "darkraise-ui/components/number-input"
import type { ReactNode } from "react"

interface NumberFieldProps {
  id?: string
  // NaN is an empty box. undefined would hand the value over to NumberInput,
  // which then stops following this prop.
  value: number
  onValueChange: (value: number) => void
  min?: number
  max?: number
  step?: number
  precision?: number
  suffix?: ReactNode
  placeholder?: string
  disabled?: boolean
  className?: string
  "aria-label"?: string
  "aria-invalid"?: boolean
  "aria-describedby"?: string
}

// NumberInput reports each number as it is typed and clamps to min and max
// only when the field loses focus.
export function NumberField({ id, value, onValueChange, min, max, step, precision, suffix, placeholder, disabled, className, ...aria }: NumberFieldProps) {
  return (
    <NumberInput
      className={className}
      value={value}
      onValueChange={(d) => onValueChange(d.valueAsNumber)}
      min={min}
      max={max}
      step={step}
      precision={precision}
      disabled={disabled}
    >
      <NumberInputControl>
        <NumberInputField id={id} placeholder={placeholder} {...aria} />
        {suffix !== undefined && <NumberInputSuffix>{suffix}</NumberInputSuffix>}
        <NumberInputTriggerGroup>
          <NumberInputIncrementTrigger />
          <NumberInputDecrementTrigger />
        </NumberInputTriggerGroup>
      </NumberInputControl>
    </NumberInput>
  )
}
