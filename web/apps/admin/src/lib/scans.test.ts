// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { formatScanSize, pdfFilename, validateScanFiles } from "./scans"

const jpeg = [255, 216, 255, 224]
const png = [137, 80, 78, 71, 13, 10, 26, 10]
const close = vi.fn()
const decode = vi.fn()

function file(bytes = jpeg, name = "page.jpg", type = "image/jpeg") {
  return new File([new Uint8Array(bytes)], name, { type })
}

beforeEach(() => {
  close.mockReset()
  decode.mockReset().mockResolvedValue({ width: 100, height: 200, close })
  vi.stubGlobal("createImageBitmap", decode)
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

it("rejects an empty selection and more than 100 files before decoding", async () => {
  await expect(validateScanFiles([])).rejects.toThrow("at least one")
  await expect(
    validateScanFiles(Array.from({ length: 101 }, () => file()))
  ).rejects.toThrow("100 images")
  expect(decode).not.toHaveBeenCalled()
})

it("accepts exactly 100 files and preserves decoding order", async () => {
  const files = Array.from({ length: 100 }, (_, index) =>
    file(jpeg, `${index}.jpg`)
  )
  await expect(validateScanFiles(files)).resolves.toBeUndefined()
  expect(decode.mock.calls.map(([value]) => value)).toEqual(files)
  expect(close).toHaveBeenCalledTimes(100)
})

it("enforces the total byte limit across files including the exact boundary", async () => {
  const first = file()
  const second = file(png)
  vi.spyOn(first, "size", "get").mockReturnValue(50 * 1024 * 1024)
  const size = vi.spyOn(second, "size", "get").mockReturnValue(50 * 1024 * 1024)
  await expect(validateScanFiles([first, second])).resolves.toBeUndefined()
  decode.mockClear()
  size.mockReturnValue(50 * 1024 * 1024 + 1)
  await expect(validateScanFiles([first, second])).rejects.toThrow("100 MiB")
  expect(decode).not.toHaveBeenCalled()
})

it.each([[], [71, 73, 70, 56, 57, 97], [137, 80, 78, 71, 0, 0, 0, 0]])(
  "rejects spoofed or empty JPEG files by their bytes: %j",
  async (...bytes) => {
    await expect(validateScanFiles([file(bytes, "spoof.jpg")])).rejects.toThrow(
      "“spoof.jpg” must contain a JPEG or PNG"
    )
    expect(decode).not.toHaveBeenCalled()
  }
)

it.each([jpeg, png])(
  "accepts valid magic without trusting MIME or extension: %j",
  async (...bytes) => {
    await expect(
      validateScanFiles([
        file(bytes, "unknown.bin", "application/octet-stream"),
      ])
    ).resolves.toBeUndefined()
    expect(close).toHaveBeenCalledOnce()
  }
)

it("reports a named decoding error for corrupt image data", async () => {
  decode.mockRejectedValue(new Error("decoder internals"))
  await expect(validateScanFiles([file()])).rejects.toThrow(
    "“page.jpg” could not be decoded"
  )
})

it.each([
  { width: 5000, height: 5001, message: "25 megapixel" },
  { width: 0, height: 100, message: "invalid image dimensions" },
  { width: 100, height: 0, message: "invalid image dimensions" },
])(
  "rejects invalid dimensions $width × $height and closes the bitmap",
  async ({ width, height, message }) => {
    decode.mockResolvedValue({ width, height, close })
    await expect(validateScanFiles([file()])).rejects.toThrow(message)
    expect(close).toHaveBeenCalledOnce()
  }
)

it("allows exactly 25 megapixels and closes the bitmap", async () => {
  decode.mockResolvedValue({ width: 5000, height: 5000, close })
  await expect(validateScanFiles([file()])).resolves.toBeUndefined()
  expect(close).toHaveBeenCalledOnce()
})

it.each(["load", "error"])(
  "cleans up fallback object URLs after image %s",
  async (event) => {
    vi.stubGlobal("createImageBitmap", undefined)
    const createObjectURL = vi.fn().mockReturnValue("blob:scan-test")
    const revokeObjectURL = vi.fn()
    vi.stubGlobal("URL", { createObjectURL, revokeObjectURL })
    const image = document.createElement("img")
    Object.defineProperties(image, {
      naturalWidth: { value: 5000 },
      naturalHeight: { value: 5000 },
    })
    vi.stubGlobal("Image", function () {
      return image
    })
    const validation = validateScanFiles([file()])
    const result =
      event === "error"
        ? expect(validation).rejects.toThrow("could not be decoded")
        : expect(validation).resolves.toBeUndefined()
    await vi.waitFor(() =>
      expect(image.getAttribute("src")).toBe("blob:scan-test")
    )
    image.dispatchEvent(new Event(event))
    await result
    expect(revokeObjectURL).toHaveBeenCalledExactlyOnceWith("blob:scan-test")
    expect(image.onload).toBeNull()
    expect(image.onerror).toBeNull()
    expect(image.getAttribute("src")).toBe("")
  }
)

it.each([
  [0, "0 B"],
  [1024, "1.0 KiB"],
  [1024 * 1024, "1.0 MiB"],
])("formats %i bytes as %s", (bytes, expected) => {
  expect(formatScanSize(bytes)).toBe(expected)
})

it.each([
  [" page.jpg ", "page.pdf"],
  ["folder/page.one.png", "page.one.pdf"],
  ["C:\\folder\\page.png", "page.pdf"],
  [".png", "document.pdf"],
  ["document", "document.pdf"],
  ["😀".repeat(252) + ".jpg", "😀".repeat(251) + ".pdf"],
])("derives the PDF name from %s", (name, expected) => {
  expect(pdfFilename(name)).toBe(expected)
})
