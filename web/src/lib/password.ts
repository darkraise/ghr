// Mirrors webui.validLength: the daemon counts characters (code points), not
// bytes or UTF-16 units.
export function passwordLengthOk(password: string): boolean {
  const n = [...password].length
  return n >= 12 && n <= 1024
}
