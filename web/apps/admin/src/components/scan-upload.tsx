import { useEffect, useRef, useState, type FormEvent } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  ArrowDown,
  ArrowUp,
  CheckCircle2,
  Download,
  FileStack,
  FileText,
  Trash2,
  Upload,
} from "lucide-react"
import {
  adminOrganizationsQueryOptions,
  adminDocumentQueryOptions,
  adminDocumentContentURL,
  uploadAdminDocument,
} from "@workspace/api"
import type { Document, Organization } from "@workspace/models"
import {
  Alert,
  AlertDescription,
  AlertTitle,
} from "@workspace/ui/components/alert"
import { Badge } from "@workspace/ui/components/badge"
import { Button } from "@workspace/ui/components/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card"
import {
  Field,
  FieldDescription,
  FieldLabel,
} from "@workspace/ui/components/field"
import { Input } from "@workspace/ui/components/input"
import {
  NativeSelect,
  NativeSelectOption,
} from "@workspace/ui/components/native-select"
import { Spinner } from "@workspace/ui/components/spinner"
import { formatScanSize, pdfFilename, validateScanFiles } from "@/lib/scans"

type Page = { id: string; file: File }
type Submission = { document: Document; organization: Organization }

export function ScanUpload() {
  const organizations = useQuery(adminOrganizationsQueryOptions())
  const [organizationID, setOrganizationID] = useState("")
  const [pages, setPages] = useState<Page[]>([])
  const [checking, setChecking] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [error, setError] = useState("")
  const [submission, setSubmission] = useState<Submission>()
  const busy = useRef(false)
  const active = useRef(true)
  const input = useRef<HTMLInputElement>(null)
  const organization = organizations.data?.find(
    (org) => org.id === organizationID
  )
  const locked = checking || uploading
  const bytes = pages.reduce((total, page) => total + page.file.size, 0)

  useEffect(() => {
    active.current = true
    return () => {
      active.current = false
    }
  }, [])

  async function addFiles(files: File[]) {
    if (busy.current || !files.length || submission) return
    busy.current = true
    setChecking(true)
    setError("")
    try {
      const selection = [...pages.map((page) => page.file), ...files]
      await validateScanFiles(selection)
      if (active.current)
        setPages((current) => [
          ...current,
          ...files.map((file) => ({ id: crypto.randomUUID(), file })),
        ])
    } catch (cause) {
      if (active.current)
        setError(
          cause instanceof Error
            ? cause.message
            : "These images couldn't be read. Choose JPEG or PNG scans."
        )
    } finally {
      busy.current = false
      if (active.current) setChecking(false)
    }
  }

  function move(index: number, delta: number) {
    setPages((current) => {
      const reordered = [...current]
      const [page] = reordered.splice(index, 1)
      reordered.splice(index + delta, 0, page!)
      return reordered
    })
  }

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (busy.current || !organization || !pages.length || submission) return
    busy.current = true
    setUploading(true)
    setError("")
    try {
      const document = await uploadAdminDocument(
        organization.id,
        pages.map((page) => page.file)
      )
      if (active.current) {
        setSubmission({ document, organization })
        setPages([])
      }
    } catch (cause) {
      if (active.current)
        setError(
          cause instanceof Error
            ? cause.message
            : "We couldn't confirm the upload. Check before uploading again to avoid a duplicate."
        )
    } finally {
      busy.current = false
      if (active.current) setUploading(false)
    }
  }

  return (
    <section className="space-y-8">
      <div className="space-y-2">
        <p className="text-sm font-medium text-muted-foreground">Mail intake</p>
        <h1 className="text-3xl font-semibold tracking-tight">Upload scans</h1>
        <p className="max-w-2xl text-muted-foreground">
          Choose the recipient organization and arrange the scanned pages. We'll
          combine them into one PDF.
        </p>
      </div>
      {submission ? (
        <UploadResult
          submission={submission}
          onNew={() => {
            setSubmission(undefined)
            setError("")
          }}
        />
      ) : (
        <form
          onSubmit={(event) => void submit(event)}
          className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_18rem]"
        >
          <div className="min-w-0 space-y-6">
            <Card>
              <CardHeader>
                <CardTitle>1. Choose a recipient</CardTitle>
                <CardDescription>
                  The document will belong to this organization.
                </CardDescription>
              </CardHeader>
              <CardContent>
                <Field>
                  <FieldLabel htmlFor="recipient">Organization</FieldLabel>
                  <NativeSelect
                    id="recipient"
                    value={organizationID}
                    className="w-full"
                    required
                    disabled={
                      locked ||
                      !organizations.isSuccess ||
                      !organizations.data.length
                    }
                    onChange={(event) => setOrganizationID(event.target.value)}
                  >
                    <NativeSelectOption value="">
                      {organizations.isPending
                        ? "Loading organizations…"
                        : "Select an organization"}
                    </NativeSelectOption>
                    {organizations.data?.map((org) => (
                      <NativeSelectOption key={org.id} value={org.id}>
                        {org.name} ({org.slug})
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                  {organizations.isError && (
                    <div className="space-y-2">
                      <p role="alert" className="text-sm text-destructive">
                        {organizations.error.message}
                      </p>
                      <Button
                        type="button"
                        variant="outline"
                        onClick={() => void organizations.refetch()}
                      >
                        Retry organizations
                      </Button>
                    </div>
                  )}
                  {organizations.isSuccess && !organizations.data.length && (
                    <FieldDescription>
                      No organizations are available for upload.
                    </FieldDescription>
                  )}
                </Field>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>2. Add and arrange pages</CardTitle>
                <CardDescription>
                  One image per scanned side. Pages appear in the PDF in the
                  order below.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-5">
                <div
                  className={`rounded-xl border-2 border-dashed px-5 py-8 text-center transition-colors ${dragging ? "border-primary bg-primary/5" : "border-border bg-muted/20"}`}
                  onDragOver={(event) => {
                    event.preventDefault()
                    if (!locked) setDragging(true)
                  }}
                  onDragLeave={() => setDragging(false)}
                  onDrop={(event) => {
                    event.preventDefault()
                    setDragging(false)
                    void addFiles(Array.from(event.dataTransfer.files))
                  }}
                >
                  <Upload
                    className="mx-auto mb-3 size-6 text-muted-foreground"
                    aria-hidden="true"
                  />
                  <p className="font-medium">Drop page images here</p>
                  <p className="mt-1 mb-4 text-sm text-muted-foreground">
                    JPEG or PNG · Up to 100 pages · 100 MiB total
                  </p>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={locked}
                    onClick={() => input.current?.click()}
                  >
                    {pages.length ? "Add more pages" : "Choose images"}
                  </Button>
                  <Field className="sr-only">
                    <FieldLabel htmlFor="scan-files">Page images</FieldLabel>
                    <Input
                      ref={input}
                      id="scan-files"
                      type="file"
                      accept="image/jpeg,image/png,.jpg,.jpeg,.png"
                      multiple
                      disabled={locked}
                      onChange={(event) => {
                        const files = Array.from(event.target.files ?? [])
                        event.target.value = ""
                        void addFiles(files)
                      }}
                    />
                  </Field>
                </div>
                {checking && (
                  <p
                    role="status"
                    className="flex items-center gap-2 text-sm text-muted-foreground"
                  >
                    <Spinner aria-hidden="true" />
                    Checking image formats and dimensions…
                  </p>
                )}
                {pages.length > 0 && (
                  <>
                    <div className="flex items-center justify-between gap-3">
                      <p className="text-sm font-medium">
                        {pages.length} {pages.length === 1 ? "page" : "pages"}{" "}
                        selected
                      </p>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        disabled={locked}
                        onClick={() => {
                          setPages([])
                          setError("")
                        }}
                      >
                        Clear all
                      </Button>
                    </div>
                    <ol
                      aria-label="Pages in PDF order"
                      className="max-h-[32rem] space-y-2 overflow-y-auto pr-1"
                    >
                      {pages.map((page, index) => (
                        <li
                          key={page.id}
                          className="flex items-center gap-3 rounded-lg border p-3"
                        >
                          <PageThumbnail file={page.file} />
                          <div className="min-w-0 flex-1">
                            <p className="text-xs text-muted-foreground">
                              Page {index + 1}
                            </p>
                            <p
                              className="truncate text-sm font-medium"
                              title={page.file.name}
                            >
                              {page.file.name}
                            </p>
                            <p className="text-xs text-muted-foreground">
                              {formatScanSize(page.file.size)}
                            </p>
                          </div>
                          <div className="flex shrink-0 gap-1">
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon-sm"
                              aria-label={`Move page ${index + 1} up`}
                              disabled={locked || index === 0}
                              onClick={() => move(index, -1)}
                            >
                              <ArrowUp aria-hidden="true" />
                            </Button>
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon-sm"
                              aria-label={`Move page ${index + 1} down`}
                              disabled={locked || index === pages.length - 1}
                              onClick={() => move(index, 1)}
                            >
                              <ArrowDown aria-hidden="true" />
                            </Button>
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon-sm"
                              aria-label={`Remove page ${index + 1}`}
                              disabled={locked}
                              onClick={() => {
                                setPages((current) =>
                                  current.filter((item) => item.id !== page.id)
                                )
                                setError("")
                              }}
                            >
                              <Trash2 aria-hidden="true" />
                            </Button>
                          </div>
                        </li>
                      ))}
                    </ol>
                  </>
                )}
                <p className="text-xs text-muted-foreground">
                  Use upright scans, up to 25 megapixels each. PDF and TIFF
                  inputs aren't supported.
                </p>
              </CardContent>
            </Card>
          </div>
          <Card className="lg:sticky lg:top-6">
            <CardHeader>
              <FileText
                className="mb-2 size-6 text-muted-foreground"
                aria-hidden="true"
              />
              <CardTitle>Ready to upload</CardTitle>
              <CardDescription>
                Review the recipient and page order before uploading.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-5">
              <dl className="space-y-4 text-sm">
                <div>
                  <dt className="text-muted-foreground">Recipient</dt>
                  <dd className="mt-1 font-medium break-words">
                    {organization?.name ?? "Choose an organization"}
                  </dd>
                  {organization && (
                    <dd className="text-xs text-muted-foreground">
                      {organization.slug}
                    </dd>
                  )}
                </div>
                <div>
                  <dt className="text-muted-foreground">Output</dt>
                  <dd className="mt-1 font-medium break-all">
                    {pages[0]
                      ? pdfFilename(pages[0].file.name)
                      : "One PDF document"}
                  </dd>
                </div>
                <div className="flex items-center justify-between">
                  <dt className="text-muted-foreground">Pages / upload size</dt>
                  <dd>
                    {pages.length} / {formatScanSize(bytes)}
                  </dd>
                </div>
              </dl>
              {error && (
                <Alert variant="destructive">
                  <AlertTitle>Upload needs attention</AlertTitle>
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}
              <Button
                type="submit"
                className="h-auto min-h-9 w-full py-2 whitespace-normal"
                disabled={
                  locked ||
                  !organization ||
                  !pages.length ||
                  organizations.isError
                }
              >
                {uploading ? (
                  <>
                    <Spinner aria-hidden="true" />
                    Uploading scans…
                  </>
                ) : (
                  <>
                    <Upload aria-hidden="true" />
                    Upload {pages.length || ""}{" "}
                    {pages.length === 1 ? "page" : "pages"}
                  </>
                )}
              </Button>
              {uploading && (
                <p role="status" className="text-sm text-muted-foreground">
                  Sending images to {organization?.name}. Keep this page open
                  until the upload is confirmed.
                </p>
              )}
              <p className="text-xs leading-relaxed text-muted-foreground">
                The original scans are retained securely. Your PDF becomes
                available after processing.
              </p>
            </CardContent>
          </Card>
        </form>
      )}
    </section>
  )
}

function PageThumbnail({ file }: { file: File }) {
  const image = useRef<HTMLImageElement>(null)
  useEffect(() => {
    const url = URL.createObjectURL(file)
    const element = image.current
    element?.setAttribute("src", url)
    return () => {
      element?.removeAttribute("src")
      URL.revokeObjectURL(url)
    }
  }, [file])
  return (
    <img
      ref={image}
      alt=""
      className="h-16 w-12 shrink-0 rounded border bg-white object-contain"
    />
  )
}

function UploadResult({
  submission,
  onNew,
}: {
  submission: Submission
  onNew: () => void
}) {
  const { organization, document } = submission
  const query = useQuery({
    ...adminDocumentQueryOptions(organization.id, document.id),
    initialData: document,
  })
  const doc = query.data
  const ready = doc.status === "uploaded"
  const failed = doc.status === "failed"
  return (
    <Card className="max-w-2xl">
      <CardHeader>
        {ready ? (
          <CheckCircle2
            aria-hidden="true"
            className="mb-3 size-8 text-emerald-600"
          />
        ) : failed ? (
          <FileStack
            aria-hidden="true"
            className="mb-3 size-8 text-destructive"
          />
        ) : (
          <Spinner aria-hidden="true" className="mb-3 size-8" />
        )}
        <CardTitle className="text-xl">
          {ready
            ? "Your PDF is ready"
            : failed
              ? "PDF processing failed"
              : "Scans received"}
        </CardTitle>
        <CardDescription>
          {doc.filename} · {organization.name}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="flex flex-wrap gap-2">
          <Badge variant="secondary">
            {doc.page_count} {doc.page_count === 1 ? "page" : "pages"}
          </Badge>
          <Badge variant="outline">
            {ready
              ? "PDF available"
              : failed
                ? "Needs attention"
                : "Preparing PDF"}
          </Badge>
        </div>
        {query.isError ? (
          <Alert variant="destructive">
            <AlertTitle>Unable to check processing</AlertTitle>
            <AlertDescription>
              {query.error.message} Your upload was received; don't upload it
              again.
            </AlertDescription>
          </Alert>
        ) : (
          <p role="status" className="text-sm text-muted-foreground">
            {ready
              ? "The document is available to the recipient organization. Text recognition and search indexing may still be running."
              : failed
                ? "The images were received, but the PDF couldn't be created. Review the source scans before starting a new upload."
                : "We're combining your scanned pages into a PDF. This page updates automatically."}
          </p>
        )}
        <div className="flex flex-wrap gap-3">
          {ready && !query.isError && (
            <Button
              nativeButton={false}
              render={
                <a
                  href={adminDocumentContentURL(organization.id, doc.id)}
                  download={doc.filename}
                />
              }
            >
              <Download aria-hidden="true" />
              Download PDF
            </Button>
          )}
          {query.isError && (
            <Button
              type="button"
              variant="outline"
              onClick={() => void query.refetch()}
            >
              Retry status check
            </Button>
          )}
          {(ready || failed) && (
            <Button type="button" variant="outline" onClick={onNew}>
              Upload another document
            </Button>
          )}
        </div>
        <p className="text-xs text-muted-foreground">
          Document reference:{" "}
          <span className="font-mono break-all">{doc.id}</span>
        </p>
      </CardContent>
    </Card>
  )
}
