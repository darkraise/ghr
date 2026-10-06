import { useNavigate, useRouter, useRouterState } from "@tanstack/react-router"
import type { RouterAdapter } from "darkraise-ui/router"
import { RouterLink } from "@/components/router-link"

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
