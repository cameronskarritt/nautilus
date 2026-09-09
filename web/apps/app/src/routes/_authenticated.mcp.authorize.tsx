import { useMutation, useQuery } from "@tanstack/react-query"
import { createFileRoute, useLocation } from "@tanstack/react-router"
import { Button } from "@workspace/ui/components/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card"

const fields = [
  "client_id",
  "redirect_uri",
  "response_type",
  "scope",
  "state",
  "code_challenge",
  "code_challenge_method",
  "resource",
] as const

type Consent = {
  client_name: string
  redirect_uri: string
  scope: string
  user_id: string
  organization: { id: string; name: string }
}

export const Route = createFileRoute("/_authenticated/mcp/authorize")({
  component: Authorize,
})

function Authorize() {
  const location = useLocation()
  // OAuth state and redirect parameters are opaque strings, not JSON values.
  const params = new URLSearchParams(location.searchStr)
  const search = Object.fromEntries(
    fields.map((key) => [key, params.get(key) ?? ""])
  )
  const request = useQuery({
    queryKey: ["mcp-consent", search],
    retry: false,
    staleTime: 0,
    queryFn: async ({ signal }): Promise<Consent> => {
      const response = await fetch(
        `/api/mcp/oauth/request?${new URLSearchParams(search)}`,
        { signal, credentials: "same-origin" }
      )
      if (!response.ok) throw new Error("This connection request is not valid.")
      return response.json()
    },
  })
  const decision = useMutation({
    mutationFn: async (choice: "allow" | "deny") => {
      if (!request.data) throw new Error("Reload this connection request.")
      const response = await fetch("/api/mcp/oauth/authorize", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          ...search,
          decision: choice,
          user_id: request.data.user_id,
          organization_id: request.data.organization.id,
        }),
      })
      if (!response.ok)
        throw new Error(
          "The connection could not be authorized. Reload and try again."
        )
      const body: { redirect_url: string } = await response.json()
      const target = new URL(body.redirect_url)
      if (!["https:", "http:"].includes(target.protocol))
        throw new Error("The connection returned an invalid redirect.")
      window.location.assign(target.href)
    },
  })

  return (
    <Card className="mx-auto max-w-lg">
      <CardHeader>
        <CardTitle className="text-2xl">Connect to Nautilus</CardTitle>
        <CardDescription>
          Review this application's access before connecting.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {request.isPending && <p role="status">Loading connection request…</p>}
        {request.isError && <p role="alert">{request.error.message}</p>}
        {request.data && (
          <>
            <p>
              <strong>{request.data.client_name}</strong> wants to connect to{" "}
              <strong>{request.data.organization.name}</strong>.
            </p>
            <ul className="list-disc space-y-2 pl-5 text-sm">
              {request.data.scope.split(" ").includes("read") && (
                <li>Read your organization's data through MCP.</li>
              )}
              {request.data.scope.split(" ").includes("write") && (
                <li>Create and update your organization's data through MCP.</li>
              )}
            </ul>
            <p className="text-sm break-all text-muted-foreground">
              You will return to {new URL(request.data.redirect_uri).host}.
            </p>
            {decision.isError && <p role="alert">{decision.error.message}</p>}
            <div className="flex gap-3">
              <Button
                disabled={decision.isPending || request.isFetching}
                onClick={() => decision.mutate("allow")}
              >
                {decision.isPending ? "Continuing…" : "Allow connection"}
              </Button>
              <Button
                variant="outline"
                disabled={decision.isPending || request.isFetching}
                onClick={() => decision.mutate("deny")}
              >
                Deny
              </Button>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}
