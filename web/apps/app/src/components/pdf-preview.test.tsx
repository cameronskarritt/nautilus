// @vitest-environment jsdom
import { act } from "react"
import { createRoot } from "react-dom/client"
import { afterEach, expect, it, vi } from "vitest"
import PDFPreview from "./pdf-preview"

const mocks = vi.hoisted(() => ({ getDocument: vi.fn() }))
vi.mock("pdfjs-dist", () => ({
  getDocument: mocks.getDocument,
  GlobalWorkerOptions: {},
}))
let root: ReturnType<typeof createRoot> | undefined
let container: HTMLDivElement | undefined

afterEach(async () => {
  await act(async () => root?.unmount())
  container?.remove()
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

async function render() {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true)
  container = document.createElement("div")
  document.body.append(container)
  root = createRoot(container)
  const blob = new Blob()
  Object.defineProperty(blob, "arrayBuffer", {
    value: async () => new ArrayBuffer(0),
  })
  await act(async () => root!.render(<PDFPreview blob={blob} />))
  return container
}

it("navigates PDF pages, zooms, and releases rendering resources", async () => {
  const cancel = vi.fn()
  const destroy = vi.fn().mockResolvedValue(undefined)
  const renderPage = vi
    .fn()
    .mockReturnValue({ promise: Promise.resolve(), cancel })
  const getPage = vi.fn().mockResolvedValue({
    getViewport: ({ scale }: { scale: number }) => ({
      width: 612 * scale,
      height: 792 * scale,
    }),
    render: renderPage,
    getTextContent: async () => ({ items: [{ str: "Accessible page text" }] }),
  })
  mocks.getDocument.mockReturnValue({
    promise: Promise.resolve({ numPages: 2, getPage }),
    destroy,
  })
  const view = await render()
  expect(view.textContent).toContain("Page 1 of 2")
  expect(view.textContent).toContain("Accessible page text")
  expect(
    view.querySelector<HTMLButtonElement>('[aria-label="Previous page"]')
      ?.disabled
  ).toBe(true)
  await act(async () =>
    view.querySelector<HTMLButtonElement>('[aria-label="Next page"]')!.click()
  )
  expect(view.textContent).toContain("Page 2 of 2")
  expect(
    view.querySelector<HTMLButtonElement>('[aria-label="Next page"]')?.disabled
  ).toBe(true)
  await act(async () =>
    view.querySelector<HTMLButtonElement>('[aria-label="Zoom in"]')!.click()
  )
  expect(view.textContent).toContain("125%")
  expect(getPage).toHaveBeenCalledWith(2)
  await act(async () => root!.render(null))
  expect(cancel).toHaveBeenCalled()
  expect(destroy).toHaveBeenCalledOnce()
})

it("shows a download fallback for unreadable or password-protected PDFs", async () => {
  mocks.getDocument.mockReturnValue({
    promise: Promise.reject(new Error("password required")),
    destroy: vi.fn(),
  })
  const view = await render()
  expect(view.querySelector('[role="alert"]')?.textContent).toContain(
    "This PDF couldn't be previewed"
  )
  expect(view.querySelector("canvas")).toBeNull()
})
