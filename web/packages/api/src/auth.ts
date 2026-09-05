import { queryOptions } from "@tanstack/react-query"
import type {
  EnvResponse,
  Organization,
  SessionResponse,
  User,
} from "@workspace/models"

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

function isUser(value: unknown): value is User {
  return (
    isObject(value) &&
    typeof value.id === "string" &&
    value.id.length > 0 &&
    (value.email === undefined || typeof value.email === "string") &&
    (value.username === undefined || typeof value.username === "string") &&
    typeof value.auth_provider === "string" &&
    ["local", "google", "microsoft", "github", "apple"].includes(
      value.auth_provider
    ) &&
    typeof value.verified === "boolean" &&
    typeof value.admin === "boolean" &&
    typeof value.mfa_enabled === "boolean" &&
    typeof value.created_at === "string"
  )
}

function isOrganization(value: unknown): value is Organization {
  return (
    isObject(value) &&
    typeof value.id === "string" &&
    value.id.length > 0 &&
    typeof value.slug === "string" &&
    typeof value.name === "string" &&
    (value.plan === undefined || typeof value.plan === "string") &&
    typeof value.personal === "boolean" &&
    typeof value.created_at === "string"
  )
}

export function envQueryOptions() {
  return queryOptions({
    queryKey: ["env"],
    retry: false,
    staleTime: 0,
    queryFn: async ({ signal }): Promise<EnvResponse> => {
      const response = await fetch("/api/env", {
        signal,
        credentials: "same-origin",
      })
      if (!response.ok) {
        throw new Error(`Environment request failed (${response.status})`)
      }

      const body: unknown = await response.json()
      if (
        !isObject(body) ||
        typeof body.version !== "number" ||
        !Number.isInteger(body.version) ||
        !isObject(body.auth) ||
        !Array.isArray(body.auth.sso_providers) ||
        !body.auth.sso_providers.every(
          (provider) => typeof provider === "string"
        )
      ) {
        throw new Error("Invalid environment response")
      }

      return {
        version: body.version,
        auth: { sso_providers: body.auth.sso_providers },
      }
    },
  })
}

export function sessionQueryOptions() {
  return queryOptions({
    queryKey: ["session"],
    retry: false,
    staleTime: 0,
    queryFn: async ({ signal }): Promise<SessionResponse | null> => {
      const response = await fetch("/api/users/me", {
        signal,
        credentials: "same-origin",
      })
      if (response.status === 401) return null
      if (!response.ok) {
        throw new Error(`Session request failed (${response.status})`)
      }

      const body: unknown = await response.json()
      if (
        !isObject(body) ||
        !isUser(body.user) ||
        (body.organization !== null && !isOrganization(body.organization)) ||
        typeof body.assumed !== "boolean" ||
        !isObject(body.flags) ||
        !Object.values(body.flags).every((flag) => typeof flag === "boolean")
      ) {
        throw new Error("Invalid session response")
      }

      return {
        user: body.user,
        organization: body.organization,
        assumed: body.assumed,
        flags: body.flags as Record<string, boolean>,
      }
    },
  })
}

export async function logout() {
  const response = await fetch("/api/auth/sessions", {
    method: "DELETE",
    credentials: "same-origin",
  })
  if (!response.ok && response.status !== 401) {
    throw new Error(`Sign out failed (${response.status})`)
  }
}

function hasUnsafeCharacters(value: string) {
  return [...value].some(
    (character) =>
      character === "\\" ||
      character.charCodeAt(0) < 32 ||
      (character.charCodeAt(0) >= 127 && character.charCodeAt(0) <= 159)
  )
}

export function safeRedirectPath(value: unknown): string {
  const fallback = "/dashboard"
  if (
    typeof value !== "string" ||
    !value.startsWith("/") ||
    value.startsWith("//") ||
    hasUnsafeCharacters(value)
  ) {
    return fallback
  }

  try {
    const pathname = value.split(/[?#]/, 1)[0] ?? ""
    // Reject encoded separators, dot segments, and nested encodings before URL normalization.
    if (/%(?:2f|5c|2e|25)/i.test(pathname)) return fallback
    if (hasUnsafeCharacters(decodeURIComponent(value))) return fallback

    const url = new URL(value, "https://frontend.invalid")
    if (url.pathname.startsWith("//")) return fallback
    if (/^\/login(?:\/|$)/i.test(decodeURIComponent(url.pathname)))
      return fallback
    return `${url.pathname}${url.search}${url.hash}`
  } catch {
    return fallback
  }
}

export function googleSignInURL(origin: string, redirectPath = "/dashboard") {
  const frontend = new URL(origin)
  if (
    !["http:", "https:"].includes(frontend.protocol) ||
    frontend.username ||
    frontend.password
  ) {
    throw new Error("Invalid frontend origin")
  }

  const redirect = new URL(safeRedirectPath(redirectPath), frontend.origin)
  const search = new URLSearchParams({ redirect: redirect.href })
  return `/api/auth/sso/google?${search}`
}
