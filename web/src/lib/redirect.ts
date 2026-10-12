// A placeholder origin: a target that resolves onto it is a path on this site.
// It has to be parsed as the browser will, because the browser drops tabs and
// newlines and reads "\" as "/", so "/<tab>/host" goes to another origin.
const BASE = "http://ghr.invalid"

// Only a path on this site, returned in the form the browser parsed it.
export function safeRedirect(target: string | undefined): string {
  if (!target?.startsWith("/")) return "/"
  try {
    const url = new URL(target, BASE)
    return url.origin === BASE ? url.pathname + url.search + url.hash : "/"
  } catch {
    return "/"
  }
}

// The search for /login that brings the visitor back to href after logging in;
// the dashboard is where login lands anyway, so it needs none.
export function loginSearch(href: string): { redirect?: string } {
  return href === "/" ? {} : { redirect: href }
}
