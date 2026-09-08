import { queryOptions } from "@tanstack/react-query"
import type { Document, Organization } from "@workspace/models"
import { isOrganization } from "./auth"
import { isDocument } from "./documents"

const uploadErrors: Record<string, [number, string]> = {
  "DOC-04": [400, "Choose one or more JPEG or PNG page images."],
  "DOC-05": [415, "The upload must contain JPEG or PNG page images."],
  "DOC-06": [413, "Page images must not exceed 100 MiB combined."],
  "DOC-07": [
    400,
    "Each filename must contain 1–255 characters without control characters.",
  ],
  "DOC-08": [503, "Document storage is unavailable. Please try again later."],
  "DOC-09": [
    503,
    "Document processing is unavailable. Please try again later.",
  ],
  "DOC-10": [
    422,
    "Each page must be a valid JPEG or PNG image of at most 25 megapixels.",
  ],
  "DOC-11": [400, "A document can contain at most 100 page images."],
}

async function request(path: string, init: RequestInit = {}) {
  const uploading = init.method === "POST"
  const fallback = uploading
    ? "The upload could not be confirmed. It may have been accepted; check before uploading again."
    : "Admin data couldn't be loaded. Please try again."
  let response: Response
  try {
    response = await fetch(path, {
      ...init,
      credentials: "same-origin",
      cache: "no-store",
    })
  } catch {
    throw new Error(fallback)
  }
  if (response.ok) return response
  switch (response.status) {
    case 401:
      throw new Error("Your session has expired. Sign in again.")
    case 403:
      throw new Error("You don't have administrator access.")
    case 404:
      throw new Error("This organization or document is no longer available.")
  }
  if (uploading) {
    const body: unknown = await response.json().catch(() => null)
    if (
      typeof body === "object" &&
      body !== null &&
      "errors" in body &&
      Array.isArray(body.errors)
    ) {
      for (const detail of body.errors) {
        if (
          typeof detail !== "object" ||
          detail === null ||
          !("code" in detail) ||
          typeof detail.code !== "string"
        )
          continue
        const error = Object.hasOwn(uploadErrors, detail.code)
          ? uploadErrors[detail.code]
          : undefined
        if (error?.[0] === response.status) throw new Error(error[1])
      }
    }
  }
  throw new Error(fallback)
}

async function readDocument(response: Response): Promise<Document> {
  const body: unknown = await response.json().catch(() => null)
  if (
    typeof body !== "object" ||
    body === null ||
    !("document" in body) ||
    !isDocument(body.document)
  ) {
    throw new Error("Invalid document response")
  }
  return body.document
}

function documentsURL(organizationID: string) {
  return `/api/admin/organizations/${encodeURIComponent(organizationID)}/documents`
}

export function adminOrganizationsQueryOptions() {
  return queryOptions({
    queryKey: ["admin-organizations"],
    retry: false,
    queryFn: async ({ signal }): Promise<Organization[]> => {
      const response = await request("/api/admin/organizations", { signal })
      const body: unknown = await response.json().catch(() => null)
      if (
        typeof body !== "object" ||
        body === null ||
        !("organizations" in body) ||
        !Array.isArray(body.organizations) ||
        !body.organizations.every(isOrganization)
      ) {
        throw new Error("Invalid organization list response")
      }
      return body.organizations
    },
  })
}

// A failed response may follow acceptance; never automatically retry an upload.
export async function uploadAdminDocument(
  organizationID: string,
  files: File[]
): Promise<Document> {
  const body = new FormData()
  for (const file of files) body.append("file", file)
  const response = await request(documentsURL(organizationID), {
    method: "POST",
    body,
  })
  try {
    return await readDocument(response)
  } catch {
    throw new Error(
      "The upload could not be confirmed. It may have been accepted; check before uploading again."
    )
  }
}

export function adminDocumentQueryOptions(organizationID: string, id: string) {
  return queryOptions({
    queryKey: ["admin-document", organizationID, id],
    retry: false,
    queryFn: async ({ signal }): Promise<Document> =>
      readDocument(
        await request(
          `${documentsURL(organizationID)}/${encodeURIComponent(id)}`,
          { signal }
        )
      ),
    refetchInterval: (query) =>
      query.state.data?.status === "uploading" ? 2000 : false,
  })
}

export function adminDocumentContentURL(organizationID: string, id: string) {
  return `${documentsURL(organizationID)}/${encodeURIComponent(id)}/content`
}
