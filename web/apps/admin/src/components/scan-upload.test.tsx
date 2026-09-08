// @vitest-environment jsdom
import { act } from "react"
import { createRoot } from "react-dom/client"
import { QueryClientProvider } from "@tanstack/react-query"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { createQueryClient } from "@workspace/api"
import { validateScanFiles } from "@/lib/scans"
import { ScanUpload } from "./scan-upload"

vi.mock("@/lib/scans", async (original) => ({
  ...(await original<typeof import("@/lib/scans")>()),
  validateScanFiles: vi.fn().mockResolvedValue(undefined),
}))
const organizations = [
  {
    id: "org-one",
    name: "First Organization",
    slug: "first",
    personal: false,
    created_at: "2026-09-07T00:00:00Z",
  },
  {
    id: "org-two",
    name: "Second Organization",
    slug: "second",
    personal: false,
    created_at: "2026-09-07T00:00:00Z",
  },
]
const doc = {
  id: "11111111-1111-4111-8111-111111111111",
  status: "uploading",
  filename: "scan.pdf",
  content_type: "application/pdf",
  size: 0,
  page_count: 2,
  created_at: "2026-09-07T00:00:00Z",
  updated_at: "2026-09-07T00:00:00Z",
}
let client: ReturnType<typeof createQueryClient>
let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
let request: ReturnType<typeof vi.fn<typeof fetch>>
let post: () => Promise<Response>
let status: () => Promise<Response>
beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true)
  vi.spyOn(window, "scrollTo").mockImplementation(() => {})
  vi.mocked(validateScanFiles).mockReset().mockResolvedValue(undefined)
  client = createQueryClient()
  container = document.createElement("div")
  document.body.append(container)
  root = createRoot(container)
  post = async () => Response.json({ document: doc }, { status: 202 })
  status = async () =>
    Response.json({ document: { ...doc, status: "uploaded", size: 10 } })
  request = vi.fn(async (url, init) => {
    if (init?.method === "POST") return post()
    if (String(url) === "/api/admin/organizations")
      return Response.json({ organizations })
    return status()
  })
  vi.stubGlobal("fetch", request)
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL = vi.fn(() => "blob:scan")
      static revokeObjectURL = vi.fn()
    }
  )
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  container.remove()
  vi.restoreAllMocks()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})
async function tick(ms = 1) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <ScanUpload />
      </QueryClientProvider>
    )
  )
  await tick()
}
function button(text: string) {
  const result = [...container.querySelectorAll("button")].find(
    (element) =>
      element.textContent === text ||
      element.getAttribute("aria-label") === text
  )
  if (!result) throw new Error(`Missing button: ${text}`)
  return result
}
async function click(text: string) {
  await act(async () => button(text).click())
  await tick()
}
async function selectOrg() {
  await act(async () => {
    const select = container.querySelector<HTMLSelectElement>("#recipient")!
    select.value = "org-two"
    select.dispatchEvent(new Event("change", { bubbles: true }))
  })
}
async function choose(names: string[]) {
  const files = names.map(
    (name) => new File([name], name, { type: "image/png" })
  )
  await act(async () => {
    const input = container.querySelector<HTMLInputElement>("#scan-files")!
    Object.defineProperty(input, "files", { configurable: true, value: files })
    input.dispatchEvent(new Event("change", { bubbles: true }))
  })
  await tick()
}
async function submit() {
  await act(async () => {
    container
      .querySelector("form")!
      .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }))
  })
  await tick()
}
function pages() {
  return [
    ...container.querySelectorAll(
      'ol[aria-label="Pages in PDF order"] li p[title]'
    ),
  ].map((element) => element.textContent)
}
function posts() {
  return request.mock.calls.filter(([, init]) => init?.method === "POST")
}

it("chooses a recipient and appends, reorders, and removes pages in multipart order", async () => {
  await render()
  expect(
    container.querySelector<HTMLButtonElement>('button[type="submit"]')!
      .disabled
  ).toBe(true)
  await selectOrg()
  await choose(["one.png", "two.png"])
  await choose(["three.png"])
  expect(pages()).toEqual(["one.png", "two.png", "three.png"])
  expect(
    vi.mocked(validateScanFiles).mock.calls[1]![0].map((file) => file.name)
  ).toEqual(["one.png", "two.png", "three.png"])
  await click("Move page 3 up")
  await click("Remove page 1")
  expect(pages()).toEqual(["three.png", "two.png"])
  await submit()
  expect(posts()).toHaveLength(1)
  expect(posts()[0]![0]).toBe("/api/admin/organizations/org-two/documents")
  const body = posts()[0]![1]!.body as FormData
  expect(body.getAll("file").map((file) => (file as File).name)).toEqual([
    "three.png",
    "two.png",
  ])
  expect(URL.revokeObjectURL).toHaveBeenCalledTimes(3)
})

it("preserves existing pages on canceled or invalid selection and clears explicitly", async () => {
  await render()
  await choose(["good.png"])
  await choose([])
  expect(pages()).toEqual(["good.png"])
  vi.mocked(validateScanFiles).mockRejectedValueOnce(
    new Error("Choose JPEG or PNG scans.")
  )
  await choose(["bad.png"])
  expect(pages()).toEqual(["good.png"])
  expect(container.textContent).toContain("Choose JPEG or PNG scans.")
  await click("Clear all")
  expect(pages()).toEqual([])
  expect(container.textContent).not.toContain("Choose JPEG or PNG scans.")
  expect(URL.revokeObjectURL).toHaveBeenCalledTimes(1)
})

it("locks changes and rejects duplicate submits while the upload is pending", async () => {
  let resolve!: (response: Response) => void
  post = () =>
    new Promise((done) => {
      resolve = done
    })
  await render()
  await selectOrg()
  await choose(["one.png", "two.png"])
  await submit()
  expect(container.textContent).toContain("Uploading scans…")
  expect(
    container.querySelector<HTMLSelectElement>("#recipient")!.disabled
  ).toBe(true)
  expect(
    container.querySelector<HTMLInputElement>("#scan-files")!.disabled
  ).toBe(true)
  for (const label of [
    "Add more pages",
    "Clear all",
    "Move page 2 up",
    "Remove page 1",
  ])
    expect(button(label).disabled).toBe(true)
  await submit()
  await choose(["ignored.png"])
  expect(posts()).toHaveLength(1)
  expect(pages()).toEqual(["one.png", "two.png"])
  await act(async () =>
    resolve(Response.json({ document: doc }, { status: 202 }))
  )
  await tick()
  expect(container.textContent).toContain("Scans received")
})

it("polls to a ready PDF with a download scoped to the selected organization", async () => {
  await render()
  await selectOrg()
  await choose(["one.png"])
  await submit()
  expect(container.textContent).toContain("Scans received")
  expect(container.querySelector("a[download]")).toBeNull()
  await tick(2001)
  expect(container.textContent).toContain("Your PDF is ready")
  expect(container.querySelector("a[download]")?.getAttribute("href")).toBe(
    `/api/admin/organizations/org-two/documents/${doc.id}/content`
  )
  const count = request.mock.calls.length
  await tick(6000)
  expect(request).toHaveBeenCalledTimes(count)
  expect(posts()).toHaveLength(1)
})

it("retries only status reads after a polling failure", async () => {
  status = async () => new Response("secret", { status: 500 })
  await render()
  await selectOrg()
  await choose(["one.png"])
  await submit()
  await tick(2001)
  expect(container.textContent).toContain("Unable to check processing")
  expect(container.textContent).toContain(
    "Your upload was received; don't upload it again."
  )
  expect(container.textContent).not.toContain("secret")
  status = async () =>
    Response.json({ document: { ...doc, status: "uploaded" } })
  await click("Retry status check")
  expect(container.textContent).toContain("Your PDF is ready")
  expect(posts()).toHaveLength(1)
})

it("retains pages after an ambiguous upload error without automatic retry", async () => {
  post = async () => new Response("secret", { status: 500 })
  await render()
  await selectOrg()
  await choose(["one.png"])
  await submit()
  expect(pages()).toEqual(["one.png"])
  expect(container.textContent).toContain("may have been accepted")
  expect(container.textContent).not.toContain("secret")
  await tick(10000)
  expect(posts()).toHaveLength(1)
  expect(button("Clear all").disabled).toBe(false)
})

it("shows a processing failure and stops polling", async () => {
  status = async () => Response.json({ document: { ...doc, status: "failed" } })
  await render()
  await selectOrg()
  await choose(["one.png"])
  await submit()
  await tick(2001)
  expect(container.textContent).toContain("PDF processing failed")
  expect(container.querySelector("a[download]")).toBeNull()
  const count = request.mock.calls.length
  await tick(6000)
  expect(request).toHaveBeenCalledTimes(count)
  await click("Upload another document")
  expect(pages()).toEqual([])
})

it("disables the recipient selector for an empty organization list", async () => {
  request.mockResolvedValueOnce(Response.json({ organizations: [] }))
  await render()
  expect(container.textContent).toContain(
    "No organizations are available for upload."
  )
  expect(
    container.querySelector<HTMLSelectElement>("#recipient")!.disabled
  ).toBe(true)
})
it("offers an explicit organization retry after failure", async () => {
  request.mockResolvedValueOnce(new Response("secret", { status: 500 }))
  await render()
  expect(container.textContent).toContain("Admin data couldn't be loaded")
  expect(
    container.querySelector<HTMLSelectElement>("#recipient")!.disabled
  ).toBe(true)
  await click("Retry organizations")
  expect(
    container.querySelector<HTMLSelectElement>("#recipient")!.disabled
  ).toBe(false)
})

it("shows public image validation errors while retaining the selected pages", async () => {
  post = async () =>
    Response.json(
      { message: "secret", errors: [{ code: "DOC-10", message: "secret" }] },
      { status: 422 }
    )
  await render()
  await selectOrg()
  await choose(["one.png"])
  await submit()
  expect(pages()).toEqual(["one.png"])
  expect(container.textContent).toContain(
    "Each page must be a valid JPEG or PNG image of at most 25 megapixels."
  )
  expect(container.textContent).not.toContain("secret")
  expect(posts()).toHaveLength(1)
})

it("prevents submit and page changes while images are being validated", async () => {
  await render()
  await selectOrg()
  await choose(["one.png"])
  let resolve!: () => void
  vi.mocked(validateScanFiles).mockImplementationOnce(
    () =>
      new Promise<void>((done) => {
        resolve = done
      })
  )
  await choose(["two.png"])
  expect(container.textContent).toContain(
    "Checking image formats and dimensions…"
  )
  expect(button("Clear all").disabled).toBe(true)
  expect(
    container.querySelector<HTMLButtonElement>('button[type="submit"]')!
      .disabled
  ).toBe(true)
  await submit()
  expect(posts()).toHaveLength(0)
  await act(async () => resolve())
  await tick()
  expect(pages()).toEqual(["one.png", "two.png"])
})
