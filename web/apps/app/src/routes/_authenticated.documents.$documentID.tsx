import { useQuery } from "@tanstack/react-query"
import { createFileRoute, Link } from "@tanstack/react-router"
import { ArrowLeft, Download } from "lucide-react"
import { documentQueryOptions, documentContentURL } from "@workspace/api"
import { Button } from "@workspace/ui/components/button"
import { DocumentPreview } from "@/components/document-preview"

import { formatSize } from "@/lib/documents"

export const Route = createFileRoute("/_authenticated/documents/$documentID")({
  component: DocumentViewer,
})

function DocumentViewer() {
  const { documentID } = Route.useParams()
  const { session } = Route.useRouteContext()
  return (
    <section className="space-y-6">
      <Link
        to="/documents"
        className="inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft aria-hidden="true" className="size-4" />
        All documents
      </Link>
      {session.organization ? (
        <Viewer
          key={`${session.organization.id}/${documentID}`}
          organizationID={session.organization.id}
          id={documentID}
        />
      ) : (
        <p role="status">Select an organization to view its documents.</p>
      )}
    </section>
  )
}

function Viewer({
  organizationID,
  id,
}: {
  organizationID: string
  id: string
}) {
  const query = useQuery(documentQueryOptions(organizationID, id))
  if (query.isPending) return <p role="status">Loading document…</p>
  if (query.isError)
    return (
      <div className="space-y-3">
        <h1 className="text-2xl font-semibold">Unable to open document</h1>
        <p role="alert">{query.error.message}</p>
        <Button variant="outline" onClick={() => void query.refetch()}>
          Try again
        </Button>
      </div>
    )
  const doc = query.data
  return (
    <>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 space-y-2">
          <h1 className="text-2xl font-semibold tracking-tight break-words">
            {doc.filename}
          </h1>
          <p className="text-sm text-muted-foreground">
            {formatSize(doc.size)} · Added{" "}
            {new Date(doc.created_at).toLocaleDateString()}
          </p>
        </div>
        {doc.status === "uploaded" && (
          <Button
            variant="outline"
            render={
              <a href={documentContentURL(doc.id)} download={doc.filename} />
            }
            nativeButton={false}
          >
            <Download aria-hidden="true" className="size-4" />
            Download
          </Button>
        )}
      </div>
      {doc.status === "uploaded" ? (
        <DocumentPreview
          key={doc.updated_at}
          organizationID={organizationID}
          document={doc}
        />
      ) : (
        <p role="status">
          {doc.status === "uploading" ? "Uploading…" : "Upload failed."}
        </p>
      )}
    </>
  )
}
