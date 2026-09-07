import { useInfiniteQuery } from "@tanstack/react-query"
import { createFileRoute, Link } from "@tanstack/react-router"
import { FileText, ArrowUpRight } from "lucide-react"
import { documentsQueryOptions } from "@workspace/api"
import { Button } from "@workspace/ui/components/button"
import { formatSize } from "@/lib/documents"

export const Route = createFileRoute("/_authenticated/documents/")({
  component: Documents,
})

function Documents() {
  const { session } = Route.useRouteContext()
  if (!session.organization)
    return <p role="status">Select an organization to view its documents.</p>
  return (
    <Library
      key={session.organization.id}
      organizationID={session.organization.id}
      name={session.organization.name}
    />
  )
}

function Library({
  organizationID,
  name,
}: {
  organizationID: string
  name: string
}) {
  const query = useInfiniteQuery(documentsQueryOptions(organizationID))
  const docs = query.data?.pages.flatMap((page) => page.data) ?? []
  return (
    <section className="space-y-6">
      <div className="space-y-2">
        <p className="text-sm text-muted-foreground">{name}</p>
        <h1 className="text-3xl font-semibold tracking-tight">Documents</h1>
        <p className="text-muted-foreground">
          Open, read, and download your documents.
        </p>
      </div>
      {query.isPending && <p role="status">Loading documents…</p>}
      {query.isError && (
        <div className="space-y-3">
          <p role="alert">{query.error.message}</p>
          <Button variant="outline" onClick={() => void query.refetch()}>
            Try again
          </Button>
        </div>
      )}
      {query.isSuccess && docs.length === 0 && (
        <div className="rounded-xl border border-dashed px-6 py-16 text-center">
          <FileText
            aria-hidden="true"
            className="mx-auto mb-4 size-8 text-muted-foreground"
          />
          <h2 className="font-medium">No documents yet</h2>
          <p className="mt-2 text-sm text-muted-foreground">
            Documents added to this organization will appear here.
          </p>
        </div>
      )}
      {docs.length > 0 && (
        <ul className="divide-y rounded-xl border">
          {docs.map((doc) => (
            <li key={doc.id}>
              <Link
                to="/documents/$documentID"
                params={{ documentID: doc.id }}
                className="flex items-center gap-4 px-5 py-5 transition-colors hover:bg-muted/50 focus-visible:outline-2 focus-visible:outline-ring"
              >
                <FileText
                  aria-hidden="true"
                  className="size-6 shrink-0 text-muted-foreground"
                />
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium">{doc.filename}</p>
                  <p className="mt-1 text-sm text-muted-foreground">
                    {formatSize(doc.size)} ·{" "}
                    {new Date(doc.created_at).toLocaleDateString()}
                  </p>
                </div>
                <ArrowUpRight
                  aria-hidden="true"
                  className="size-4 shrink-0 text-muted-foreground"
                />
              </Link>
            </li>
          ))}
        </ul>
      )}
      {query.hasNextPage && (
        <Button
          variant="outline"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          {query.isFetchingNextPage ? "Loading…" : "Load more"}
        </Button>
      )}
    </section>
  )
}
