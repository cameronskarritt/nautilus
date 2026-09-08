import { afterEach, expect, it, vi } from "vitest"
import { QueryObserver } from "@tanstack/react-query"
import type { Document } from "@workspace/models"
import {
  createQueryClient,
  adminDocumentContentURL,
  adminDocumentQueryOptions,
  adminOrganizationsQueryOptions,
  uploadAdminDocument,
} from "./index"

const doc: Document = {
  id: "11111111-1111-4111-8111-111111111111",
  status: "uploading",
  filename: "scan.pdf",
  content_type: "application/pdf",
  size: 0,
  page_count: 2,
  created_at: "2026-09-07T00:00:00Z",
  updated_at: "2026-09-07T00:00:00Z",
}
const organization = {
  id: "org-1",
  name: "Example",
  slug: "example",
  personal: false,
  created_at: "2026-09-07T00:00:00Z",
}
const clients: ReturnType<typeof createQueryClient>[] = []
function client() {
  const c = createQueryClient()
  clients.push(c)
  return c
}
afterEach(() => {
  for (const c of clients) c.clear()
  clients.length = 0
  vi.unstubAllGlobals()
})

it("loads organizations without HTTP caching", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValue(Response.json({ organizations: [organization] }))
  vi.stubGlobal("fetch", fetch)
  expect(await client().fetchQuery(adminOrganizationsQueryOptions())).toEqual([
    organization,
  ])
  expect(fetch).toHaveBeenCalledWith(
    "/api/admin/organizations",
    expect.objectContaining({
      credentials: "same-origin",
      cache: "no-store",
      signal: expect.any(AbortSignal),
    })
  )
})
it.each([
  { organizations: [{}] },
  { organizations: [{ ...organization, personal: "false" }] },
  { organizations: null },
])("rejects malformed organizations", async (body) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(body)))
  await expect(
    client().fetchQuery(adminOrganizationsQueryOptions())
  ).rejects.toThrow("Invalid organization list response")
})
it("uploads ordered repeated file parts with browser-generated multipart headers", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValue(Response.json({ document: doc }, { status: 202 }))
  vi.stubGlobal("fetch", fetch)
  const files = [
    new File(["second"], "second.png", { type: "image/png" }),
    new File(["first"], "first.jpg", { type: "image/jpeg" }),
  ]
  expect(await uploadAdminDocument("org/1", files)).toEqual(doc)
  expect(fetch).toHaveBeenCalledTimes(1)
  const [url, init] = fetch.mock.calls[0]!
  expect(url).toBe("/api/admin/organizations/org%2F1/documents")
  expect(init).toMatchObject({
    method: "POST",
    credentials: "same-origin",
    cache: "no-store",
  })
  expect(init.headers).toBeUndefined()
  expect([...init.body.keys()]).toEqual(["file", "file"])
  const parts = init.body.getAll("file") as File[]
  expect(parts.map((file) => file.name)).toEqual(["second.png", "first.jpg"])
  expect(await Promise.all(parts.map((file) => file.text()))).toEqual([
    "second",
    "first",
  ])
})
it.each([
  [400, "DOC-04", "Choose one or more"],
  [415, "DOC-05", "upload must contain"],
  [413, "DOC-06", "100 MiB"],
  [400, "DOC-07", "filename"],
  [503, "DOC-08", "storage is unavailable"],
  [503, "DOC-09", "processing is unavailable"],
  [422, "DOC-10", "25 megapixels"],
  [400, "DOC-11", "100 page images"],
  [401, "unknown", "session has expired"],
  [403, "unknown", "administrator access"],
  [404, "unknown", "no longer available"],
  [500, "DOC-04", "may have been accepted"],
  [400, "unknown", "may have been accepted"],
])(
  "maps %s/%s to safe messages without retrying",
  async (status, code, message) => {
    const fetch = vi
      .fn()
      .mockResolvedValue(
        Response.json(
          { message: "secret", errors: [{ code, message: "secret" }] },
          { status: Number(status) }
        )
      )
    vi.stubGlobal("fetch", fetch)
    await expect(uploadAdminDocument("org", [])).rejects.toThrow(
      String(message)
    )
    expect(fetch).toHaveBeenCalledTimes(1)
  }
)
it("handles ambiguous network failures without retrying or exposing details", async () => {
  const fetch = vi.fn().mockRejectedValue(new Error("secret"))
  vi.stubGlobal("fetch", fetch)
  await expect(uploadAdminDocument("org", [])).rejects.toThrow(
    "may have been accepted"
  )
  expect(fetch).toHaveBeenCalledTimes(1)
})
it.each<Document["status"]>(["uploading", "uploaded", "failed"])(
  "scopes reads and polls only uploading documents (%s)",
  async (status) => {
    const document = { ...doc, status }
    const fetch = vi.fn().mockResolvedValue(Response.json({ document }))
    vi.stubGlobal("fetch", fetch)
    const c = client(),
      options = adminDocumentQueryOptions("org/1", doc.id)
    expect(await c.fetchQuery(options)).toEqual(document)
    expect(options.retry).toBe(false)
    expect(
      c.getQueryData(adminDocumentQueryOptions("org/2", doc.id).queryKey)
    ).toBeUndefined()
    const query = new QueryObserver(c, options).getCurrentQuery()
    if (typeof options.refetchInterval !== "function")
      throw new Error("Missing polling callback")
    expect(options.refetchInterval(query)).toBe(
      status === "uploading" ? 2000 : false
    )
    expect(fetch).toHaveBeenCalledWith(
      `/api/admin/organizations/org%2F1/documents/${doc.id}`,
      expect.objectContaining({ credentials: "same-origin", cache: "no-store" })
    )
    expect(adminDocumentContentURL("org/1", "id/2")).toBe(
      "/api/admin/organizations/org%2F1/documents/id%2F2/content"
    )
  }
)
it.each([undefined, -1, 1.5, 101, "2"])(
  "rejects invalid page_count %s",
  async (page_count) => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockImplementation(() =>
          Promise.resolve(Response.json({ document: { ...doc, page_count } }))
        )
    )
    await expect(
      client().fetchQuery(adminDocumentQueryOptions("org", doc.id))
    ).rejects.toThrow("Invalid document response")
    await expect(uploadAdminDocument("org", [])).rejects.toThrow(
      "may have been accepted"
    )
  }
)
