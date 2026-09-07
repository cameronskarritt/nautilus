import type { Document } from "@workspace/models"

export function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export function previewKind(doc: Pick<Document, "content_type" | "size">) {
  const type = doc.content_type.split(";", 1)[0]?.trim().toLowerCase()
  if (type === "application/pdf") return "pdf"
  if (
    [
      "image/png",
      "image/jpeg",
      "image/gif",
      "image/webp",
      "image/bmp",
    ].includes(type ?? "")
  )
    return "image"
  if (type === "text/plain" && doc.size <= 1024 * 1024) return "text"
  return null
}

export function decodeDocumentText(buffer: ArrayBuffer) {
  const bytes = new Uint8Array(buffer)
  const encoding =
    bytes[0] === 0xff && bytes[1] === 0xfe
      ? "utf-16le"
      : bytes[0] === 0xfe && bytes[1] === 0xff
        ? "utf-16be"
        : "utf-8"
  return new TextDecoder(encoding).decode(bytes)
}
