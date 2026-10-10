import { expect } from "vitest"

// A StatusPill's tint sits on the word's parent and its tone on the icon.
export function expectPill(label: HTMLElement, fill: string, text: string) {
  expect(label.parentElement).toHaveClass("rounded-full", fill)
  expect(label.parentElement?.querySelector("svg")).toHaveClass(text)
}
