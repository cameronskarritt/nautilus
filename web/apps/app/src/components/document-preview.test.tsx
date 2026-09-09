// @vitest-environment jsdom
import { act } from "react"
import { createRoot } from "react-dom/client"
import { QueryClientProvider } from "@tanstack/react-query"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { createQueryClient, documentContentQueryOptions } from "@workspace/api"
import { DocumentPreview } from "./document-preview"

const doc = {
  status: "uploaded" as const,
  id: "11111111-1111-4111-8111-111111111111",
  filename: "letter.pdf",
  content_type: "application/pdf",
  size: 5,
  sha256: "",
  page_count: 0,
  created_at: "2026-09-07T00:00:00Z",
  updated_at: "2026-09-07T00:00:00Z",
}
let client: ReturnType<typeof createQueryClient>
let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
beforeEach(() => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true)
  client = createQueryClient()
  container = document.createElement("div")
  document.body.append(container)
  root = createRoot(container)
  vi.stubGlobal("fetch", vi.fn())
  vi.stubGlobal(
    "URL",
    class extends URL {
      static createObjectURL = vi.fn(() => "blob:preview")
      static revokeObjectURL = vi.fn()
    }
  )
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  container.remove()
  vi.unstubAllGlobals()
})

async function render(content_type: string, blob?: Blob) {
  const document = { ...doc, content_type }
  if (blob)
    client.setQueryData(
      documentContentQueryOptions("org", document).queryKey,
      blob
    )
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <DocumentPreview organizationID="org" document={document} />
      </QueryClientProvider>
    )
  )
}

it("creates and revokes the image preview URL", async () => {
  await render("image/png", new Blob(["image"]))
  expect(container.querySelector("img")?.getAttribute("src")).toBe(
    "blob:preview"
  )
  expect(container.querySelector("img")?.alt).toBe("letter.pdf")
  await act(async () => root.render(null))
  expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:preview")
})

it("renders text as text without executing markup", async () => {
  const blob = new Blob()
  Object.defineProperty(blob, "arrayBuffer", {
    value: async () =>
      new TextEncoder().encode('<script>bad()</script><img src="x">').buffer,
  })
  await render("text/plain", blob)
  expect(container.querySelector("pre")?.textContent).toContain(
    "<script>bad()</script>"
  )
  expect(container.querySelector("script, img, iframe")).toBeNull()
})

it("offers a download fallback without fetching unsupported active content", async () => {
  await render("text/html")
  expect(container.textContent).toContain("Preview unavailable")
  expect(container.querySelector("iframe, img")).toBeNull()
  expect(fetch).not.toHaveBeenCalled()
})

it("zooms image previews and resets to fit width", async () => {
  await render("image/png", new Blob(["image"]))
  await act(async () =>
    container
      .querySelector<HTMLButtonElement>('[aria-label="Zoom in"]')!
      .click()
  )
  expect(container.querySelector("img")?.style.width).toBe("125%")
  await act(async () =>
    [...container.querySelectorAll("button")]
      .find((button) => button.textContent === "Reset")!
      .click()
  )
  expect(container.querySelector("img")?.style.width).toBe("100%")
})

it.each(["uploading", "failed"] as const)(
  "does not render or fetch a preview for %s documents",
  async (status) => {
    await act(async () =>
      root.render(
        <QueryClientProvider client={client}>
          <DocumentPreview organizationID="org" document={{ ...doc, status }} />
        </QueryClientProvider>
      )
    )
    expect(container.textContent).toBe("")
    expect(fetch).not.toHaveBeenCalled()
  }
)
