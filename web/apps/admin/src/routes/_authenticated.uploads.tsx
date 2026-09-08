import { createFileRoute } from "@tanstack/react-router"
import { ScanUpload } from "@/components/scan-upload"

export const Route = createFileRoute("/_authenticated/uploads")({
  component: ScanUpload,
})
