import { useInfiniteQuery } from "@tanstack/react-query"
import { createFileRoute, Link } from "@tanstack/react-router"
import { FileText } from "lucide-react"
import { documentsQueryOptions } from "@workspace/api"
import { Button } from "@workspace/ui/components/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@workspace/ui/components/table"
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
        <div className="overflow-hidden rounded-lg border">
          <Table aria-label="Documents">
            <TableHeader>
              <TableRow>
                <TableHead scope="col" className="w-full pl-4">
                  Name
                </TableHead>
                <TableHead scope="col">Type</TableHead>
                <TableHead scope="col" className="text-right">
                  Size
                </TableHead>
                <TableHead scope="col" className="pr-4 text-right">
                  Added
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {docs.map((doc) => (
                <TableRow key={doc.id}>
                  <TableCell className="pl-4">
                    <Link
                      to="/documents/$documentID"
                      params={{ documentID: doc.id }}
                      className="inline-flex max-w-64 items-center gap-3 rounded-sm py-2 font-medium hover:underline focus-visible:outline-2 focus-visible:outline-ring sm:max-w-96"
                    >
                      <FileText
                        aria-hidden="true"
                        className="size-4 shrink-0 text-muted-foreground"
                      />
                      <span className="truncate" title={doc.filename}>
                        {doc.filename}
                      </span>
                    </Link>
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {doc.content_type}
                  </TableCell>
                  <TableCell className="text-right text-muted-foreground tabular-nums">
                    {formatSize(doc.size)}
                  </TableCell>
                  <TableCell className="pr-4 text-right text-muted-foreground">
                    <time dateTime={doc.created_at}>
                      {new Date(doc.created_at).toLocaleDateString()}
                    </time>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
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
