import { afterEach, describe, expect, it, vi } from "vitest"
import type { SessionResponse } from "@workspace/models"

import {
  createQueryClient,
  envQueryOptions,
  googleSignInURL,
  logout,
  safeRedirectPath,
  sessionQueryOptions,
} from "./index"

const session: SessionResponse = {
  user: {
    id: "1a6f1768-7c7e-43c9-a310-4e48768ad13e",
    email: "person@example.com",
    auth_provider: "google",
    verified: true,
    admin: false,
    mfa_enabled: false,
    created_at: "2026-09-05T00:00:00Z",
  },
  organization: null,
  assumed: false,
  flags: { preview: true },
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe("sessionQueryOptions", () => {
  it("reads the backend session and revalidates on the next fetch", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockImplementation(async () => Response.json(session))
    vi.stubGlobal("fetch", fetchMock)

    const client = createQueryClient()
    await expect(client.fetchQuery(sessionQueryOptions())).resolves.toEqual(
      session
    )
    await client.fetchQuery(sessionQueryOptions())
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it("returns null for an expired session", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn<typeof fetch>()
        .mockResolvedValue(new Response(null, { status: 401 }))
    )

    const client = createQueryClient()
    await expect(client.fetchQuery(sessionQueryOptions())).resolves.toBeNull()
  })

  it.each([undefined, "free"])(
    "reads administrator access and the current organization with plan %j",
    async (plan) => {
      const adminSession: SessionResponse = {
        ...session,
        user: { ...session.user, admin: true },
        organization: {
          id: "552530c1-8f81-4a82-af09-e09520d9bf9c",
          slug: "example",
          name: "Example",
          plan,
          personal: false,
          created_at: "2026-09-05T00:00:00Z",
        },
        assumed: true,
      }
      vi.stubGlobal(
        "fetch",
        vi.fn<typeof fetch>().mockResolvedValue(Response.json(adminSession))
      )

      await expect(
        createQueryClient().fetchQuery(sessionQueryOptions())
      ).resolves.toEqual(adminSession)
    }
  )

  it.each([null, false, 1, []])(
    "rejects a non-string organization plan: %j",
    async (plan) => {
      vi.stubGlobal(
        "fetch",
        vi.fn<typeof fetch>().mockResolvedValue(
          Response.json({
            ...session,
            organization: {
              id: "552530c1-8f81-4a82-af09-e09520d9bf9c",
              slug: "example",
              name: "Example",
              plan,
              personal: false,
              created_at: "2026-09-05T00:00:00Z",
            },
          })
        )
      )

      await expect(
        createQueryClient().fetchQuery(sessionQueryOptions())
      ).rejects.toThrow("Invalid session response")
    }
  )

  it("rejects server errors without retrying or accepting cached access", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValue(new Response(null, { status: 503 }))
    vi.stubGlobal("fetch", fetchMock)

    const client = createQueryClient()
    client.setQueryData(sessionQueryOptions().queryKey, session)
    await expect(client.fetchQuery(sessionQueryOptions())).rejects.toThrow(
      "Session request failed (503)"
    )
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it("propagates network failures", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>().mockRejectedValue(new TypeError("Network failure"))
    )

    await expect(
      createQueryClient().fetchQuery(sessionQueryOptions())
    ).rejects.toThrow("Network failure")
  })

  it.each([undefined, null, "true", "false", 0, 1])(
    "rejects a non-boolean administrator claim: %j",
    async (admin) => {
      vi.stubGlobal(
        "fetch",
        vi.fn<typeof fetch>().mockResolvedValue(
          Response.json({
            ...session,
            user: { ...session.user, admin },
          })
        )
      )

      await expect(
        createQueryClient().fetchQuery(sessionQueryOptions())
      ).rejects.toThrow("Invalid session response")
    }
  )

  it.each([
    null,
    {},
    { ...session, user: null },
    { ...session, user: { ...session.user, id: "" } },
    { ...session, user: { ...session.user, verified: "true" } },
    { ...session, user: { ...session.user, auth_provider: 1 } },
    { ...session, assumed: "false" },
    { ...session, flags: { preview: "true" } },
    { ...session, organization: {} },
  ])("rejects malformed session data: %j", async (body) => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>().mockResolvedValue(Response.json(body))
    )

    await expect(
      createQueryClient().fetchQuery(sessionQueryOptions())
    ).rejects.toThrow("Invalid session response")
  })

  it("forwards session cookies and query cancellation", async () => {
    const fetchMock = vi.fn<typeof fetch>().mockImplementation(
      (_input, init) =>
        new Promise((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () => {
            reject(new DOMException("Request aborted", "AbortError"))
          })
        })
    )
    vi.stubGlobal("fetch", fetchMock)

    const client = createQueryClient()
    const request = client.fetchQuery(sessionQueryOptions())
    const rejection = expect(request).rejects.toThrow()
    expect(fetchMock).toHaveBeenCalledWith("/api/users/me", {
      signal: expect.any(AbortSignal),
      credentials: "same-origin",
    })
    const signal = fetchMock.mock.calls[0]?.[1]?.signal
    expect(signal?.aborted).toBe(false)
    await client.cancelQueries({ queryKey: sessionQueryOptions().queryKey })
    expect(signal?.aborted).toBe(true)
    await rejection
  })
})

describe("envQueryOptions", () => {
  it("reads the available providers with same-origin credentials", async () => {
    const env = { version: 1, auth: { sso_providers: ["google"] } }
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValue(Response.json(env))
    vi.stubGlobal("fetch", fetchMock)

    await expect(
      createQueryClient().fetchQuery(envQueryOptions())
    ).resolves.toEqual(env)
    expect(fetchMock).toHaveBeenCalledWith("/api/env", {
      signal: expect.any(AbortSignal),
      credentials: "same-origin",
    })
  })

  it.each([null, {}, { version: 1, auth: { sso_providers: [true] } }])(
    "rejects malformed environment data: %j",
    async (body) => {
      vi.stubGlobal(
        "fetch",
        vi.fn<typeof fetch>().mockResolvedValue(Response.json(body))
      )

      await expect(
        createQueryClient().fetchQuery(envQueryOptions())
      ).rejects.toThrow("Invalid environment response")
    }
  )
})

describe("safeRedirectPath", () => {
  it.each([
    "/dashboard",
    "/dashboard?tab=activity#recent",
    "/projects/hello%20world",
  ])("preserves a local destination: %s", (path) => {
    expect(safeRedirectPath(path)).toBe(path)
  })

  it.each([
    undefined,
    "",
    "dashboard",
    "https://evil.example/dashboard",
    "//evil.example/dashboard",
    "/\\evil.example",
    "/dashboard\n",
    "/dashboard%0a",
    "/%2f%2fevil.example",
    "/%252f%252fevil.example",
    "/%5cevil.example",
    "/%2e%2e/login",
    "/dashboard/..//evil.example",
    "/%",
    "/login",
    "/LOGIN",
    "/login?redirect=/login",
    "/login/",
    "/dashboard/../login",
    "/%6cogin",
  ])("falls back for an unsafe destination: %j", (path) => {
    expect(safeRedirectPath(path)).toBe("/dashboard")
  })
})

describe("googleSignInURL", () => {
  it.each(["http://localhost:5173", "http://localhost:5174"])(
    "returns to the requesting frontend: %s",
    (origin) => {
      const url = new URL(
        googleSignInURL(origin, "/dashboard?tab=activity#recent"),
        origin
      )
      expect(url.pathname).toBe("/api/auth/sso/google")
      expect(url.searchParams.get("redirect")).toBe(
        `${origin}/dashboard?tab=activity#recent`
      )
    }
  )

  it("keeps hostile destinations on the trusted frontend origin", () => {
    const origin = "https://admin.example.com"
    const url = new URL(googleSignInURL(origin, "//evil.example"), origin)
    expect(url.searchParams.get("redirect")).toBe(`${origin}/dashboard`)
  })

  it.each(["javascript:alert(1)", "https://user:password@example.com"])(
    "rejects an invalid frontend origin: %s",
    (origin) => {
      expect(() => googleSignInURL(origin)).toThrow("Invalid frontend origin")
    }
  )
})

describe("logout", () => {
  it.each([200, 204, 401])(
    "clears a session with status %i",
    async (status) => {
      const fetchMock = vi
        .fn<typeof fetch>()
        .mockResolvedValue(new Response(null, { status }))
      vi.stubGlobal("fetch", fetchMock)

      await expect(logout()).resolves.toBeUndefined()
      expect(fetchMock).toHaveBeenCalledWith("/api/auth/sessions", {
        method: "DELETE",
        credentials: "same-origin",
      })
    }
  )

  it("propagates server failures", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn<typeof fetch>()
        .mockResolvedValue(new Response(null, { status: 503 }))
    )
    await expect(logout()).rejects.toThrow("Sign out failed (503)")
  })
})
