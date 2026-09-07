import { useQuery } from "@tanstack/react-query"
import {
  createFileRoute,
  Navigate,
  Outlet,
  redirect,
} from "@tanstack/react-router"
import { sessionQueryOptions } from "@workspace/api"
import { SignOut } from "@/components/sign-out"

export const Route = createFileRoute("/_authenticated")({
  beforeLoad: ({ context }) => {
    if (!context.session?.user.admin) {
      throw redirect({ to: "/forbidden", replace: true })
    }
  },
  component: AuthenticatedLayout,
})

function AuthenticatedLayout() {
  const session = useQuery({ ...sessionQueryOptions(), refetchOnMount: false })
  if (!session.data) return null
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
