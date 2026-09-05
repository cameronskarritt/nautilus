import { createFileRoute } from "@tanstack/react-router"
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card"

export const Route = createFileRoute("/_authenticated/dashboard")({
  component: Dashboard,
})

function Dashboard() {
  return (
    <section className="max-w-2xl space-y-6">
      <h1 className="text-3xl font-semibold tracking-tight">Administration</h1>
      <Card>
        <CardHeader>
          <CardTitle>Welcome to Nautilus Admin</CardTitle>
        </CardHeader>
        <CardContent className="text-muted-foreground">
          You are signed in with administrator access.
        </CardContent>
      </Card>
    </section>
  )
}
