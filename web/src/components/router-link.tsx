import { Link } from "@tanstack/react-router"
import type { RouterLinkProps } from "darkraise-ui/router"

export function RouterLink({ to, activeClassName, activeExact, ...rest }: RouterLinkProps) {
  return (
    <Link
      to={to}
      activeOptions={activeExact ? { exact: true } : undefined}
      activeProps={activeClassName ? { className: activeClassName } : undefined}
      {...rest}
    />
  )
}
