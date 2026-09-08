const maxFiles = 100
const maxBytes = 100 * 1024 * 1024
const maxPixels = 25_000_000

export async function validateScanFiles(files: File[]): Promise<void> {
  if (!files.length) throw new Error("Choose at least one JPEG or PNG image.")
  if (files.length > maxFiles)
    throw new Error("Choose no more than 100 images.")
  if (files.reduce((total, file) => total + file.size, 0) > maxBytes) {
    throw new Error("The selected images must total 100 MiB or less.")
  }
  for (const file of files) {
    let header: Uint8Array
    try {
      header = await new Promise<Uint8Array>((resolve, reject) => {
        const reader = new FileReader()
        reader.onload = () =>
          resolve(new Uint8Array(reader.result as ArrayBuffer))
        reader.onerror = () => reject(reader.error)
        reader.onabort = () => reject(new Error("File reading canceled"))
        reader.readAsArrayBuffer(file.slice(0, 8))
      })
    } catch {
      throw new Error(`Could not read “${file.name}”. Choose the file again.`)
    }
    const jpeg = header[0] === 0xff && header[1] === 0xd8 && header[2] === 0xff
    const png = [137, 80, 78, 71, 13, 10, 26, 10].every(
      (byte, index) => header[index] === byte
    )
    if (!jpeg && !png) {
      throw new Error(`“${file.name}” must contain a JPEG or PNG image.`)
    }
    let dimensions: { width: number; height: number }
    try {
      dimensions = await imageDimensions(file)
    } catch {
      throw new Error(
        `“${file.name}” could not be decoded. Choose a valid JPEG or PNG image.`
      )
    }
    if (dimensions.width <= 0 || dimensions.height <= 0) {
      throw new Error(`“${file.name}” has invalid image dimensions.`)
    }
    if (dimensions.width * dimensions.height > maxPixels) {
      throw new Error(`“${file.name}” exceeds the 25 megapixel limit.`)
    }
  }
}

async function imageDimensions(file: File) {
  if (typeof createImageBitmap === "function") {
    const bitmap = await createImageBitmap(file)
    try {
      return { width: bitmap.width, height: bitmap.height }
    } finally {
      bitmap.close()
    }
  }
  const url = URL.createObjectURL(file)
  const image = new Image()
  try {
    return await new Promise<{ width: number; height: number }>(
      (resolve, reject) => {
        image.onload = () =>
          resolve({ width: image.naturalWidth, height: image.naturalHeight })
        image.onerror = () => reject(new Error("Image decoding failed"))
        image.src = url
      }
    )
  } finally {
    image.onload = null
    image.onerror = null
    image.src = ""
    URL.revokeObjectURL(url)
  }
}

export function formatScanSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`
}

export function pdfFilename(filename: string): string {
  const basename =
    filename.trim().replaceAll("\\", "/").split("/").at(-1)?.trim() ?? ""
  const extension = basename.lastIndexOf(".")
  const stem =
    (extension < 0 ? basename : basename.slice(0, extension)) || "document"
  return `${Array.from(stem).slice(0, 251).join("")}.pdf`
}
