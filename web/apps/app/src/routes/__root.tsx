import type { QueryClient } from "@tanstack/react-query"
import { useQuery } from "@tanstack/react-query"
import {
  createRootRouteWithContext,
  Link,
  Navigate,
  Outlet,
  redirect,
  useLocation,
  useRouter,
} from "@tanstack/react-router"
import { safeRedirectPath, sessionQueryOptions } from "@workspace/api"
import { Button } from "@workspace/ui/components/button"
import { Waves } from "lucide-react"

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()(
  {
    beforeLoad: async ({ context, location }) => {
      if (location.pathname === "/login") return { session: null }
      const session = await context.queryClient.fetchQuery(
        sessionQueryOptions()
      )
      if (!session)
        throw redirect({
          to: "/login",
          search: {
            redirect: safeRedirectPath(location.href),
            error: undefined,
          },
          replace: true,
        })
      return { session }
    },
    component: RootLayout,
    pendingComponent: () => (
      <main className="grid min-h-svh place-items-center p-6">
        <p role="status">Checking your session…</p>
      </main>
    ),
    errorComponent: SessionError,
    notFoundComponent: () => (
      <section className="space-y-4 py-12">
        <h1 className="text-2xl font-semibold">Page not found</h1>
        <Link to="/dashboard" className="underline underline-offset-4">
          Back to dashboard
        </Link>
      </section>
    ),
  }
)

function SessionError() {
  const router = useRouter()
  return (
    <main className="grid min-h-svh place-items-center p-6">
      <div className="space-y-4">
        <h1 className="text-2xl font-semibold">Unable to check your session</h1>
        <p role="alert">Please try again when the service is available.</p>
        <Button onClick={() => void router.invalidate()}>Try again</Button>
      </div>
    </main>
  )
}

function RootLayout() {
  const location = useLocation()
  const session = useQuery({ ...sessionQueryOptions(), refetchOnMount: false })
  if (location.pathname === "/login")
    return (
      <main className="grid min-h-svh place-items-center bg-background p-6">
        <div className="w-full max-w-md">
          <Outlet />
        </div>
      </main>
    )
  if (session.isError) return <SessionError />
  if (session.isPending)
    return (
      <main className="grid min-h-svh place-items-center p-6">
        <p role="status">Checking your session…</p>
      </main>
    )
  if (!session.data)
    return (
      <Navigate
        to="/login"
        search={{ redirect: safeRedirectPath(location.href), error: undefined }}
        replace
      />
    )
  return (
    <div className="min-h-svh bg-background">
      <header className="border-b">
        <div className="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-6 px-6 py-5">
          <Link
            to="/dashboard"
            className="flex items-center gap-2 font-semibold"
          >
            <Waves aria-hidden="true" className="size-5" />
            Nautilus
          </Link>
          <nav aria-label="Main navigation" className="flex gap-6 text-sm">
            <Link to="/dashboard" activeProps={{ className: "font-semibold" }}>
              Dashboard
            </Link>
            <Link to="/documents" activeProps={{ className: "font-semibold" }}>
              Documents
            </Link>
            <Link to="/status" activeProps={{ className: "font-semibold" }}>
              Service status
            </Link>
          </nav>
        </div>
      </header>
      <main className="mx-auto max-w-5xl px-6 py-12">
        <Outlet />
      </main>
    </div>
  )
}
