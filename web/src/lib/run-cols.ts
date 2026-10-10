// Columns of a RunRow: icon and main, plus the values when wide, plus actions.
export function runCols(wide: boolean, actions: boolean): number {
  return 2 + (wide ? 1 : 0) + (actions ? 1 : 0)
}
