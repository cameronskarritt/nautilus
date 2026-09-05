import { useQuery } from "@tanstack/react-query"
import {
  createFileRoute,
  Navigate,
  Outlet,
  redirect,
  useLocation,
  useRouter,
} from "@tanstack/react-router"
import { safeRedirectPath, sessionQueryOptions } from "@workspace/api"
import { Button } from "@workspace/ui/components/button"
import { SignOut } from "@/components/sign-out"

export const Route = createFileRoute("/_authenticated")({
  beforeLoad: async ({ context, location }) => {
    const session = await context.queryClient.fetchQuery(sessionQueryOptions())
    if (!session) {
      throw redirect({
        to: "/login",
        search: { redirect: safeRedirectPath(location.href), error: undefined },
        replace: true,
      })
    }
    if (!session.user.admin) throw redirect({ to: "/forbidden", replace: true })
    return { session }
  },
  pendingComponent: () => <p role="status">Checking your session…</p>,
  errorComponent: SessionError,
  component: AuthenticatedLayout,
})

function SessionError() {
  const router = useRouter()
  return (
    <section className="space-y-4">
      <h1 className="text-2xl font-semibold">Unable to check your session</h1>
      <p role="alert">Please try again when the service is available.</p>
      <Button onClick={() => void router.invalidate()}>Try again</Button>
    </section>
  )
}

function AuthenticatedLayout() {
  const session = useQuery({ ...sessionQueryOptions(), refetchOnMount: false })
  const location = useLocation()
  if (session.isError) return <SessionError />
  if (session.isPending) return <p role="status">Checking your session…</p>
  if (!session.data) {
    return (
      <Navigate
        to="/login"
        search={{ redirect: safeRedirectPath(location.href), error: undefined }}
        replace
      />
    )
  }
  if (!session.data.user.admin) return <Navigate to="/forbidden" replace />

  return (
    <div className="space-y-10">
      <div className="flex flex-wrap items-center justify-between gap-4 border-b pb-6">
        <p className="text-sm text-muted-foreground">
          Signed in as{" "}
          <span className="font-medium text-foreground">
            {session.data.user.email ??
              session.data.user.username ??
              "Nautilus user"}
          </span>
        </p>
        <SignOut />
      </div>
      <Outlet />
    </div>
  )
}
