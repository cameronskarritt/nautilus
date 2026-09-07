import { expect, it } from "vitest"
import { decodeDocumentText, previewKind } from "./documents"

it.each([
  ["application/pdf", 100, "pdf"],
  ["image/png", 100, "image"],
  ["image/jpeg", 100, "image"],
  ["text/plain; charset=utf-8", 100, "text"],
  ["text/plain", 1048577, null],
  ["text/html", 100, null],
  ["image/svg+xml", 100, null],
  ["application/octet-stream", 100, null],
  [
    "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    100,
    null,
  ],
])(
  "limits preview of %s (%s bytes) to safe supported formats",
  (content_type, size, expected) => {
    expect(
      previewKind({ content_type: String(content_type), size: Number(size) })
    ).toBe(expected)
  }
)

it.each([
  [[0xff, 0xfe, 0x48, 0, 0x69, 0]],
  [[0xfe, 0xff, 0, 0x48, 0, 0x69]],
  [[0x48, 0x69]],
])("decodes document text from BOM or UTF-8", (bytes) => {
  expect(decodeDocumentText(new Uint8Array(bytes).buffer)).toBe("Hi")
})
