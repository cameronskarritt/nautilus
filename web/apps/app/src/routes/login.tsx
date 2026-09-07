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

  if (session.isSuccess && !session.isFetching && session.data) {
    return <Navigate to={search.redirect} replace />
  }

  return (
    <section>
      <SignInCard
        title="Sign in to Nautilus"
        description="Sign in or create your account with Google."
        href={googleSignInURL(window.location.origin, search.redirect)}
        pending={env.isFetching || session.isFetching}
        available={
          env.isSuccess &&
          session.isSuccess &&
          env.data.auth.sso_providers.includes("google")
        }
        error={
          search.error
            ? "Google sign-in could not be completed. Please try again."
            : env.isError
              ? "We couldn't load Google sign-in. Please try again."
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
