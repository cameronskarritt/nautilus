import type { QueryClient } from "@tanstack/react-query"
import {
  createRootRouteWithContext,
  Link,
  Outlet,
} from "@tanstack/react-router"
import { Waves } from "lucide-react"

export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()(
  {
    component: RootLayout,
    notFoundComponent: () => (
      <section className="space-y-4 py-12">
        <h1 className="text-2xl font-semibold">Page not found</h1>
        <Link to="/" className="underline underline-offset-4">
          Back to overview
        </Link>
      </section>
    ),
  }
)

function RootLayout() {
  return (
    <div className="min-h-svh bg-background">
      <header className="border-b">
        <div className="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-6 px-6 py-5">
          <Link to="/" className="flex items-center gap-2 font-semibold">
            <Waves aria-hidden="true" className="size-5" />
            Nautilus
          </Link>
          <nav aria-label="Main navigation" className="flex gap-6 text-sm">
            <Link
              to="/"
              activeOptions={{ exact: true }}
              activeProps={{ className: "font-semibold" }}
            >
              Overview
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
