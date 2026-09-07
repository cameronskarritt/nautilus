import { lazy, Suspense, useEffect, useRef, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import type { Document } from "@workspace/models"
import { documentContentQueryOptions } from "@workspace/api"
import { Button } from "@workspace/ui/components/button"
import { decodeDocumentText, previewKind } from "@/lib/documents"

const PDFPreview = lazy(() => import("./pdf-preview"))

export function DocumentPreview({
  organizationID,
  document: doc,
}: {
  organizationID: string
  document: Document
}) {
  const kind = previewKind(doc)
  if (!kind)
    return (
      <div className="rounded-xl border bg-muted/30 px-6 py-20 text-center">
        <h2 className="font-medium">Preview unavailable</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          Download this document to open it on your device.
        </p>
      </div>
    )
  return <Preview organizationID={organizationID} document={doc} kind={kind} />
}

function Preview({
  organizationID,
  document: doc,
  kind,
}: {
  organizationID: string
  document: Document
  kind: "pdf" | "image" | "text"
}) {
  const query = useQuery(documentContentQueryOptions(organizationID, doc))
  if (query.isPending)
    return (
      <p role="status" className="rounded-xl border p-12 text-center">
        Loading preview…
      </p>
    )
  if (query.isError)
    return (
      <div className="space-y-3 rounded-xl border p-8">
        <p role="alert">{query.error.message}</p>
        <Button variant="outline" onClick={() => void query.refetch()}>
          Retry preview
        </Button>
      </div>
    )
  if (kind === "pdf")
    return (
      <Suspense fallback={<p role="status">Preparing PDF…</p>}>
        <PDFPreview blob={query.data} />
      </Suspense>
    )
  return <Content blob={query.data} kind={kind} filename={doc.filename} />
}

function Content({
  blob,
  kind,
  filename,
}: {
  blob: Blob
  kind: "pdf" | "image" | "text"
  filename: string
}) {
  const image = useRef<HTMLImageElement>(null)
  const [text, setText] = useState<string>()
  const [failed, setFailed] = useState(false)
  const [zoom, setZoom] = useState(100)

  useEffect(() => {
    if (kind === "text") {
      let active = true
      void blob
        .arrayBuffer()
        .then(decodeDocumentText)
        .then(
          (value) => {
            if (active) setText(value)
          },
          () => {
            if (active) setFailed(true)
          }
        )
      return () => {
        active = false
      }
    }
    const url = URL.createObjectURL(blob)
    const element = image.current
    element?.setAttribute("src", url)
    return () => {
      element?.removeAttribute("src")
      URL.revokeObjectURL(url)
    }
  }, [blob, kind])

  if (failed)
    return (
      <p role="alert" className="rounded-xl border p-8">
        This file couldn't be previewed. Use Download to open it on your device.
      </p>
    )
  if (kind === "text")
    return (
      <pre
        tabIndex={0}
        aria-label="Document text"
        className="max-h-[75svh] min-h-96 overflow-auto rounded-xl border bg-muted/20 p-6 font-mono text-sm break-words whitespace-pre-wrap focus-visible:outline-2 focus-visible:outline-ring"
      >
        {text ?? "Loading text…"}
      </pre>
    )
  return (
    <div className="overflow-hidden rounded-xl border">
      <div className="flex items-center justify-end gap-3 border-b bg-muted/30 p-3">
        <Button
          variant="outline"
          aria-label="Zoom out"
          disabled={zoom <= 50}
          onClick={() => setZoom(zoom - 25)}
        >
          −
        </Button>
        <span className="w-12 text-center text-sm" aria-live="polite">
          {zoom}%
        </span>
        <Button
          variant="outline"
          aria-label="Zoom in"
          disabled={zoom >= 200}
          onClick={() => setZoom(zoom + 25)}
        >
          +
        </Button>
        <Button variant="ghost" onClick={() => setZoom(100)}>
          Reset
        </Button>
      </div>
      <div
        tabIndex={0}
        role="region"
        aria-label="Document image"
        className="max-h-[75svh] min-h-96 overflow-auto bg-muted/20 p-4 focus-visible:outline-2 focus-visible:outline-ring"
      >
        <img
          ref={image}
          alt={filename}
          onError={() => setFailed(true)}
          className="mx-auto block max-w-none"
          style={{ width: `${zoom}%` }}
        />
      </div>
    </div>
  )
}
