import { createFileRoute } from "@tanstack/react-router"
import { SignOut } from "@/components/sign-out"

export const Route = createFileRoute("/forbidden")({ component: Forbidden })

function Forbidden() {
  return (
    <section className="max-w-xl space-y-6">
      <h1 className="text-3xl font-semibold tracking-tight">
        Administrator access required
      </h1>
      <p className="text-muted-foreground">
        This account does not have access to Nautilus Admin. Contact an
        administrator or sign out to use another Google account.
      </p>
      <SignOut />
    </section>
  )
}
