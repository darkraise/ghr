// The unsaved changes of the form page on screen, for code outside the page
// that is about to throw them away. One form page is mounted at a time.
let count = 0

export function setUnsavedCount(n: number): void {
  count = n
}

export function unsavedCount(): number {
  return count
}
