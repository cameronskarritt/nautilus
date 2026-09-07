import { useEffect, useRef, useState } from "react"
import {
  getDocument,
  GlobalWorkerOptions,
  type PDFDocumentProxy,
  type RenderTask,
} from "pdfjs-dist"
import workerURL from "pdfjs-dist/build/pdf.worker.min.mjs?url"
import { Button } from "@workspace/ui/components/button"

GlobalWorkerOptions.workerSrc = workerURL

export default function PDFPreview({ blob }: { blob: Blob }) {
  const [pdf, setPDF] = useState<PDFDocumentProxy>()
  const [failed, setFailed] = useState(false)
  const [page, setPage] = useState(1)
  const [zoom, setZoom] = useState(100)
  useEffect(() => {
    let active = true
    let task: ReturnType<typeof getDocument> | undefined
    void blob
      .arrayBuffer()
      .then(async (data) => {
        if (!active) return
        task = getDocument({
          data,
          useSystemFonts: true,
        })
        const document = await task.promise
        if (active) setPDF(document)
      })
      .catch(() => {
        if (active) setFailed(true)
      })
    return () => {
      active = false
      void task?.destroy()
    }
  }, [blob])
  if (failed)
    return (
      <p role="alert" className="rounded-xl border p-8">
        This PDF couldn't be previewed. Download it to open it on your device.
        Password-protected files must be opened on your device.
      </p>
    )
  if (!pdf) return <p role="status">Preparing PDF…</p>
  return (
    <div className="overflow-hidden rounded-xl border">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b bg-muted/30 p-3">
        <div className="flex items-center gap-3">
          <Button
            variant="outline"
            aria-label="Previous page"
            disabled={page === 1}
            onClick={() => setPage(page - 1)}
          >
            Previous
          </Button>
          <span className="text-sm" aria-live="polite">
            Page {page} of {pdf.numPages}
          </span>
          <Button
            variant="outline"
            aria-label="Next page"
            disabled={page === pdf.numPages}
            onClick={() => setPage(page + 1)}
          >
            Next
          </Button>
        </div>
        <div className="flex items-center gap-3">
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
      </div>
      <PDFPage key={`${page}/${zoom}`} pdf={pdf} page={page} zoom={zoom} />
    </div>
  )
}

function PDFPage({
  pdf,
  page,
  zoom,
}: {
  pdf: PDFDocumentProxy
  page: number
  zoom: number
}) {
  const container = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const [status, setStatus] = useState("Rendering page…")
  const [text, setText] = useState("")
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    let active = true
    let render: RenderTask | undefined
    void pdf
      .getPage(page)
      .then(async (documentPage) => {
        if (!active || !canvas.current || !container.current) return
        const base = documentPage.getViewport({ scale: 1 })
        const width = Math.max(100, container.current.clientWidth - 32)
        const scale = (Math.min(width / base.width, 1.5) * zoom) / 100
        const ratio = Math.min(window.devicePixelRatio || 1, 2)
        const viewport = documentPage.getViewport({ scale })
        canvas.current.width = Math.ceil(viewport.width * ratio)
        canvas.current.height = Math.ceil(viewport.height * ratio)
        canvas.current.style.width = `${viewport.width}px`
        canvas.current.style.height = `${viewport.height}px`
        render = documentPage.render({
          canvas: canvas.current,
          viewport,
          transform: [ratio, 0, 0, ratio, 0, 0],
        })
        await render.promise
        if (active) setStatus("")
        const content = await documentPage.getTextContent()
        if (active)
          setText(
            content.items
              .map((item) => ("str" in item ? item.str : ""))
              .join(" ")
          )
      })
      .catch(() => {
        if (active) setFailed(true)
      })
    return () => {
      active = false
      render?.cancel()
    }
  }, [pdf, page, zoom])
  return (
    <>
      {failed ? (
        <p role="alert" className="p-6">
          This page couldn't be rendered. Download the PDF to read it.
        </p>
      ) : (
        status && (
          <p role="status" className="p-3 text-center text-sm">
            {status}
          </p>
        )
      )}
      <div
        ref={container}
        tabIndex={0}
        role="region"
        aria-label={`PDF page ${page}`}
        className="max-h-[75svh] min-h-96 overflow-auto bg-muted/30 p-4 focus-visible:outline-2 focus-visible:outline-ring"
      >
        <canvas
          ref={canvas}
          aria-label={`Page ${page}`}
          className="mx-auto bg-white shadow-sm"
        />
      </div>
      {text && (
        <details className="border-t p-4">
          <summary className="cursor-pointer text-sm">Page text</summary>
          <p className="mt-3 text-sm leading-relaxed whitespace-pre-wrap">
            {text}
          </p>
        </details>
      )}
    </>
  )
}
