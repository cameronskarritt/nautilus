import { createFileRoute, Outlet, redirect } from "@tanstack/react-router"
import { safeRedirectPath } from "@workspace/api"
import { SignOut } from "@/components/sign-out"

export const Route = createFileRoute("/_authenticated")({
  beforeLoad: ({ context, location }) => {
    const { session } = context
    if (!session) {
      throw redirect({
        to: "/login",
        search: { redirect: safeRedirectPath(location.href), error: undefined },
        replace: true,
      })
    }
    return { session }
  },
  component: AuthenticatedLayout,
})

function AuthenticatedLayout() {
  const { session } = Route.useRouteContext()
  return (
    <div className="space-y-10">
      <div className="flex flex-wrap items-center justify-between gap-4 border-b pb-6">
        <p className="text-sm text-muted-foreground">
          Signed in as{" "}
          <span className="font-medium text-foreground">
            {session.user.email ?? session.user.username ?? "Nautilus user"}
          </span>
        </p>
        <SignOut />
      </div>
      <Outlet />
    </div>
  )
}
