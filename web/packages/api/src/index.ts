import { QueryClient, queryOptions } from "@tanstack/react-query"
import type { HealthResponse } from "@workspace/models"

export {
  envQueryOptions,
  googleSignInURL,
  logout,
  safeRedirectPath,
  sessionQueryOptions,
} from "./auth"

export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        retry: 1,
      },
    },
  })
}

export function healthQueryOptions() {
  return queryOptions({
    queryKey: ["health"],
    queryFn: async ({ signal }): Promise<HealthResponse> => {
      const response = await fetch("/api/health", {
        signal,
        credentials: "same-origin",
      })

      if (!response.ok) {
        throw new Error(`Health request failed (${response.status})`)
      }

      const body: unknown = await response.json()
      if (
        typeof body !== "object" ||
        body === null ||
        !("status" in body) ||
        (body.status !== "ok" && body.status !== "shutting_down")
      ) {
        throw new Error("Invalid health response")
      }

      return { status: body.status }
    },
  })
}
