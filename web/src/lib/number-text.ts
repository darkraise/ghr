// Text-valued form fields hand NumberField a number and take its value back as
// text; an empty box is NaN on the number side.
export function numberFromText(text: string): number {
  return text.trim() === "" ? Number.NaN : Number(text)
}

export function textFromNumber(n: number): string {
  return Number.isNaN(n) ? "" : String(n)
}
