import { createContext, useContext } from "react"

export interface FieldAria {
  "aria-describedby"?: string
  "aria-invalid"?: boolean
}

// Field puts its help and error in this context, so the control inside it can
// point at them: the child Field gets is often a wrapper, not the input.
export const FieldAriaContext = createContext<FieldAria>({})

export function useFieldAria(): FieldAria {
  return useContext(FieldAriaContext)
}
