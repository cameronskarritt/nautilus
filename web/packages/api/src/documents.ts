import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query"
import type { Document, DocumentPage } from "@workspace/models"

function isDocument(value: unknown): value is Document {
  if (typeof value !== "object" || value === null) return false
  const doc = value as Record<string, unknown>
  return (
    typeof doc.id === "string" &&
    /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/i.test(doc.id) &&
    (doc.status === "uploading" ||
      doc.status === "uploaded" ||
      doc.status === "failed") &&
    typeof doc.filename === "string" &&
    typeof doc.content_type === "string" &&
    typeof doc.size === "number" &&
    Number.isSafeInteger(doc.size) &&
    doc.size >= 0 &&
    typeof doc.created_at === "string" &&
    Number.isFinite(Date.parse(doc.created_at)) &&
    typeof doc.updated_at === "string" &&
    Number.isFinite(Date.parse(doc.updated_at))
  )
}

async function request(path: string, signal: AbortSignal) {
  const response = await fetch(path, {
    signal,
    credentials: "same-origin",
    cache: "no-store",
  })
  if (!response.ok) {
    switch (response.status) {
      case 401:
        throw new Error(
          "Your session has expired. Sign in again to view documents."
        )
      case 403:
        throw new Error("You don't have access to these documents.")
      case 404:
        throw new Error("This document is no longer available.")
      default:
        throw new Error("Documents couldn't be loaded. Please try again.")
    }
  }
  return response
}

export function documentsQueryOptions(organizationID: string) {
  return infiniteQueryOptions({
    queryKey: ["documents", organizationID],
    initialPageParam: "",
    retry: false,
    queryFn: async ({ signal, pageParam }): Promise<DocumentPage> => {
      const search = new URLSearchParams({ limit: "30" })
      if (pageParam) search.set("cursor", pageParam)
      const response = await request(`/api/documents?${search}`, signal)
      const body: unknown = await response.json()
      if (
        typeof body !== "object" ||
        body === null ||
        !("data" in body) ||
        !Array.isArray(body.data) ||
        !body.data.every(isDocument) ||
        !("has_more" in body) ||
        typeof body.has_more !== "boolean" ||
        (body.has_more &&
          (!("next_cursor" in body) ||
            typeof body.next_cursor !== "string" ||
            !body.next_cursor))
      ) {
        throw new Error("Invalid document list response")
      }
      return body as DocumentPage
    },
    refetchInterval: (query) =>
      query.state.data?.pages.some((page) =>
        page.data.some((doc) => doc.status === "uploading")
      )
        ? 2000
        : false,
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
  })
}

export function documentQueryOptions(organizationID: string, id: string) {
  return queryOptions({
    queryKey: ["document", organizationID, id],
    retry: false,
    queryFn: async ({ signal }): Promise<Document> => {
      const response = await request(
        `/api/documents/${encodeURIComponent(id)}`,
        signal
      )
      const body: unknown = await response.json()
      if (
        typeof body !== "object" ||
        body === null ||
        !("document" in body) ||
        !isDocument(body.document)
      ) {
        throw new Error("Invalid document response")
      }
      return body.document
    },
    refetchInterval: (query) =>
      query.state.data?.status === "uploading" ? 2000 : false,
  })
}

export function documentContentURL(id: string) {
  return `/api/documents/${encodeURIComponent(id)}/content`
}

export function documentContentQueryOptions(
  organizationID: string,
  doc: Document
) {
  return queryOptions({
    queryKey: ["document-content", organizationID, doc.id, doc.updated_at],
    retry: false,
    enabled: doc.status === "uploaded",
    gcTime: 0,
    staleTime: Infinity,
    queryFn: async ({ signal }) => {
      const response = await request(documentContentURL(doc.id), signal)
      return new Blob([await response.arrayBuffer()], {
        type: doc.content_type,
      })
    },
  })
}
