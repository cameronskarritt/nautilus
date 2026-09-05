import { useQuery } from "@tanstack/react-query"
import { createFileRoute } from "@tanstack/react-router"
import { healthQueryOptions } from "@workspace/api"
import { Badge } from "@workspace/ui/components/badge"
import { Button } from "@workspace/ui/components/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card"

export const Route = createFileRoute("/status")({
  loader: ({ context }) => {
    void context.queryClient.prefetchQuery(healthQueryOptions())
  },
  component: ServiceStatus,
})

function ServiceStatus() {
  const health = useQuery(healthQueryOptions())
  const available = health.isSuccess && health.data.status === "ok"
  const label = health.isPending
    ? "Checking"
    : available
      ? "Available"
      : "Unavailable"

  return (
    <section className="max-w-xl space-y-6">
      <h1 className="text-3xl font-semibold tracking-tight">Service status</h1>
      <Card>
        <CardHeader>
          <CardTitle>Nautilus API</CardTitle>
          <CardDescription>Connection to the Nautilus service.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4" aria-live="polite">
          <Badge variant={available ? "secondary" : "outline"}>{label}</Badge>
          <p className="text-sm text-muted-foreground">
            {health.isPending
              ? "Checking the service…"
              : available
                ? "The service is responding normally."
                : "The service is currently unavailable. Try again shortly."}
          </p>
        </CardContent>
        <CardFooter>
          <Button
            variant="outline"
            disabled={health.isFetching}
            onClick={() => void health.refetch()}
          >
            {health.isFetching ? "Checking…" : "Check again"}
          </Button>
        </CardFooter>
      </Card>
    </section>
  )
}
