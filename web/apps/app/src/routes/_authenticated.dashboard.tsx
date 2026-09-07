import { createFileRoute, Link } from "@tanstack/react-router"
import { Button } from "@workspace/ui/components/button"
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
      <h1 className="text-3xl font-semibold tracking-tight">Your workspace</h1>
      <Card>
        <CardHeader>
          <CardTitle>Welcome to Nautilus</CardTitle>
        </CardHeader>
        <CardContent className="text-muted-foreground">
          Read and download the documents in your organization.
        </CardContent>
      </Card>
      <Button render={<Link to="/documents" />} nativeButton={false}>
        View documents
      </Button>
    </section>
  )
}
