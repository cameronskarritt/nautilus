// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest"
import { createMemoryHistory, createRouter } from "@tanstack/react-router"
import {
  createQueryClient,
  safeRedirectPath,
  sessionQueryOptions,
} from "@workspace/api"
import type { SessionResponse } from "@workspace/models"
import { routeTree } from "./routeTree.gen"

const session: SessionResponse = {
  user: {
    id: "user-1",
    email: "user@example.com",
    auth_provider: "google",
    admin: true,
    verified: true,
    mfa_enabled: false,
    created_at: "2026-09-05T00:00:00Z",
  },
  organization: null,
  assumed: false,
  flags: {},
}

const clients: ReturnType<typeof createQueryClient>[] = []

afterEach(() => {
  for (const client of clients) client.clear()
  clients.length = 0
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

function routerAt(path = "/dashboard") {
  const queryClient = createQueryClient()
  clients.push(queryClient)
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: [path] }),
    isServer: false,
  })
  return { router, queryClient }
}

it("sends anonymous dashboard visits to login with the destination", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(new Response(null, { status: 401 }))
  )
  const { router } = routerAt("/dashboard?tab=overview")
  await router.load()
  expect(router.state.location.pathname).toBe("/login")
  expect(router.state.location.search).toMatchObject({
    redirect: "/dashboard?tab=overview",
  })
  expect(
    router.state.matches.some(
      (match) => match.routeId === "/_authenticated/dashboard"
    )
  ).toBe(false)
})

it("allows a permitted session to load the dashboard", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(session)))
  const { router } = routerAt()
  await router.load()
  expect(router.state.location.pathname).toBe("/dashboard")
  expect(router.state.matches.at(-1)?.status).toBe("success")
  expect(router.state.matches.at(-1)?.context).toMatchObject({
    session: { user: { id: "user-1" } },
  })
})

it("does not authorize a stale cached session after the server returns 401", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(new Response(null, { status: 401 }))
  )
  const { router, queryClient } = routerAt()
  queryClient.setQueryData(sessionQueryOptions().queryKey, session)
  await router.load()
  expect(router.state.location.pathname).toBe("/login")
  expect(queryClient.getQueryData(sessionQueryOptions().queryKey)).toBeNull()
})

it("shows an auth-check error instead of a login redirect on server failure", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(new Response(null, { status: 503 }))
  )
  const { router } = routerAt()
  await router.load()
  expect(router.state.location.pathname).toBe("/dashboard")
  expect(
    router.state.matches.some(
      (match) => match.routeId === "/_authenticated" && match.status === "error"
    )
  ).toBe(true)
  expect(router.state.matches.at(-1)?.context).not.toHaveProperty("session")
})

it("keeps signed-in non-admins out of the admin dashboard", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        Response.json({ ...session, user: { ...session.user, admin: false } })
      )
  )
  const { router } = routerAt()
  await router.load()
  expect(router.state.location.pathname).toBe("/forbidden")
  expect(
    router.state.matches.some(
      (match) => match.routeId === "/_authenticated/dashboard"
    )
  ).toBe(false)
})

it("preserves the local destination query and fragment when leaving login", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation(() => Promise.resolve(Response.json(session)))
  )
  const { router } = routerAt("/login")
  await router.load()
  await router.navigate({
    to: safeRedirectPath("/dashboard?tab=overview#details"),
  })
  expect(router.state.location.href).toBe("/dashboard?tab=overview#details")
  expect(router.state.location.search).toEqual({ tab: "overview" })
  expect(router.state.location.hash).toBe("details")
  expect(router.state.matches.at(-1)?.routeId).toBe("/_authenticated/dashboard")
})
