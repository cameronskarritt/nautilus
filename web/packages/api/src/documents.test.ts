import { afterEach, expect, it, vi } from "vitest"
import { createQueryClient } from "./index"
import {
  documentContentQueryOptions,
  documentQueryOptions,
  documentsQueryOptions,
} from "./documents"

const doc = {
  id: "11111111-1111-4111-8111-111111111111",
  filename: "letter.txt",
  content_type: "text/plain",
  size: 5,
  created_at: "2026-09-07T00:00:00Z",
  updated_at: "2026-09-07T00:00:00Z",
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

it("loads document pages with opaque cursors and organization-scoped cache keys", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(
      Response.json({ data: [doc], has_more: true, next_cursor: "opaque+/=" })
    )
    .mockResolvedValueOnce(Response.json({ data: [], has_more: false }))
  vi.stubGlobal("fetch", fetch)
  const c = client()
  const options = documentsQueryOptions("org-1")
  const result = await c.fetchInfiniteQuery({ ...options, pages: 2 })
  expect(result.pages).toHaveLength(2)
  expect(fetch.mock.calls[1]?.[0]).toBe(
    "/api/documents?limit=30&cursor=opaque%2B%2F%3D"
  )
  expect(fetch.mock.calls[0]?.[1]).toMatchObject({
    credentials: "same-origin",
    cache: "no-store",
  })
  expect(
    c.getQueryData(documentsQueryOptions("org-2").queryKey)
  ).toBeUndefined()
})

it.each([
  { data: [{ ...doc, size: -1 }], has_more: false },
  { data: [doc], has_more: true },
  { data: [{ ...doc, id: "../../auth" }], has_more: false },
  { data: [{ ...doc, created_at: "invalid" }], has_more: false },
])("rejects malformed document pages", async (body) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(body)))
  await expect(
    client().fetchInfiniteQuery(documentsQueryOptions("org"))
  ).rejects.toThrow("Invalid document list")
})

it.each([401, 403, 404, 500])(
  "rejects failed document reads (%s) without trusting server text",
  async (status) => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("secret", { status }))
    )
    await expect(
      client().fetchQuery(documentQueryOptions("org", doc.id))
    ).rejects.toThrow(
      /session has expired|don't have access|no longer available|couldn't be loaded/
    )
  }
)

it("fetches content as a typed blob without changing its bytes", async () => {
  const fetch = vi.fn().mockResolvedValue(new Response("hello"))
  vi.stubGlobal("fetch", fetch)
  const result = await client().fetchQuery(
    documentContentQueryOptions("org", doc)
  )
  expect(result.type).toBe("text/plain")
  expect(await result.text()).toBe("hello")
  expect(fetch.mock.calls[0]?.[0]).toBe(`/api/documents/${doc.id}/content`)
})
