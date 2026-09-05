import { createFileRoute, Link } from "@tanstack/react-router"
import { ArrowRight } from "lucide-react"
import { Button } from "@workspace/ui/components/button"

export const Route = createFileRoute("/")({ component: Overview })

function Overview() {
  return (
    <section className="max-w-2xl space-y-6 py-12">
      <p className="text-sm font-medium text-muted-foreground">
        Welcome to Nautilus
      </p>
      <h1 className="text-4xl font-semibold tracking-tight sm:text-5xl">
        Your workspace starts here.
      </h1>
      <p className="text-lg leading-relaxed text-muted-foreground">
        This is the starting point for your Nautilus account and organizations.
      </p>
      <Button render={<Link to="/dashboard" />} nativeButton={false}>
        Open workspace <ArrowRight data-icon="inline-end" />
      </Button>
    </section>
  )
}
