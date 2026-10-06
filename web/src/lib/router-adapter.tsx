import { Link, useNavigate, useRouter, useRouterState } from "@tanstack/react-router"
import type { RouterAdapter, RouterLinkProps } from "darkraise-ui/router"

function RouterLink({ to, activeClassName, activeExact, ...rest }: RouterLinkProps) {
  return (
    <Link
      to={to}
      activeOptions={activeExact ? { exact: true } : undefined}
      activeProps={activeClassName ? { className: activeClassName } : undefined}
      {...rest}
    />
  )
}

export const routerAdapter: RouterAdapter = {
  Link: RouterLink,
  useNavigate: () => {
    const navigate = useNavigate()
    return (to: string) => {
      void navigate({ to })
    }
  },
  usePathname: () => useRouterState({ select: (s) => s.location.pathname }),
  useBack: () => {
    const router = useRouter()
    return () => router.history.back()
  },
  useInvalidate: () => {
    const router = useRouter()
    return () => {
      void router.invalidate()
    }
  },
}
