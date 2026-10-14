import { Link, useRouter } from "@tanstack/react-router"
import type { RouterLinkProps } from "darkraise-ui/router"

export function RouterLink({ to, activeClassName, activeExact, ...rest }: RouterLinkProps) {
  const router = useRouter()
  const props = {
    activeOptions: activeExact ? { exact: true } : undefined,
    activeProps: activeClassName ? { className: activeClassName } : undefined,
    ...rest,
  }
  // The router matches routes on the path alone, so a query or hash in `to`
  // goes in the props meant for them. parseSearch gives the values the
  // route's validator expects, such as attempt=2 as the number 2.
  if (/[?#]/.test(to)) {
    const url = new URL(to, window.location.origin)
    return <Link to={url.pathname} search={router.options.parseSearch(url.search)} hash={url.hash.slice(1) || undefined} {...props} />
  }
  return <Link to={to} {...props} />
}
