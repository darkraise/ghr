// Only a path on this site: "//host" and "/\host" are read by browsers as
// another origin.
export function safeRedirect(target: string | undefined): string {
  return target && target.startsWith("/") && !target.startsWith("//") && !target.startsWith("/\\") ? target : "/"
}

// The search for /login that brings the visitor back to href after logging in;
// the dashboard is where login lands anyway, so it needs none.
export function loginSearch(href: string): { redirect?: string } {
  return href === "/" ? {} : { redirect: href }
}
