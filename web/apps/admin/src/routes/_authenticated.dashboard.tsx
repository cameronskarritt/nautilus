import { createFileRoute, Link } from "@tanstack/react-router"
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card"

import { Button } from "@workspace/ui/components/button"
import { Upload } from "lucide-react"

export const Route = createFileRoute("/_authenticated/dashboard")({
  component: Dashboard,
})

function Dashboard() {
  return (
    <section className="max-w-2xl space-y-6">
      <h1 className="text-3xl font-semibold tracking-tight">Administration</h1>
      <Card>
        <CardHeader>
          <CardTitle>Scan mail for an organization</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-muted-foreground">
            Upload scanned page images, review their order, and create a PDF for
            the recipient organization.
          </p>
          <Button nativeButton={false} render={<Link to="/uploads" />}>
            <Upload aria-hidden="true" />
            Upload scans
          </Button>
        </CardContent>
      </Card>
    </section>
  )
}
