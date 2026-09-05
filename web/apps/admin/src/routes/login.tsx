import { useQuery } from "@tanstack/react-query"
import { createFileRoute, Navigate } from "@tanstack/react-router"
import {
  envQueryOptions,
  googleSignInURL,
  safeRedirectPath,
  sessionQueryOptions,
} from "@workspace/api"
import { SignInCard } from "@workspace/ui/components/sign-in-card"

export const Route = createFileRoute("/login")({
  validateSearch: (search: Record<string, unknown>) => ({
    redirect: safeRedirectPath(
      typeof search.redirect === "string" ? search.redirect : undefined
    ),
    error: typeof search.error === "string" ? search.error : undefined,
  }),
  component: Login,
})

function Login() {
  const search = Route.useSearch()
  const env = useQuery(envQueryOptions())
  const session = useQuery(sessionQueryOptions())

  if (session.data) {
    if (!session.data.user.admin) return <Navigate to="/forbidden" replace />
    return <Navigate to={search.redirect} replace />
  }

  return (
    <section className="py-12">
      <SignInCard
        title="Sign in to Nautilus Admin"
        description="Use your Google account to access internal tools. Administrator access is required."
        href={googleSignInURL(window.location.origin, search.redirect)}
        pending={env.isPending}
        available={
          env.isSuccess && env.data.auth.sso_providers.includes("google")
        }
        error={
          search.error
            ? "Google sign-in could not be completed. Please try again."
            : session.isError
              ? "We couldn't check your session. Please try again."
              : undefined
        }
        onRetry={() => {
          void env.refetch()
          void session.refetch()
        }}
      />
    </section>
  )
}
