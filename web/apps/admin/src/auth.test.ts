// @vitest-environment jsdom
import { act, createElement } from "react"
import { createRoot, type Root } from "react-dom/client"
import { QueryClientProvider } from "@tanstack/react-query"
import { afterEach, expect, it, vi } from "vitest"
import {
  createMemoryHistory,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router"
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

const roots: Root[] = []

afterEach(async () => {
  await act(async () => {
    for (const root of roots) root.unmount()
  })
  roots.length = 0
  document.body.replaceChildren()
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

it.each([
  "/",
  "/status",
  "/forbidden",
  "/missing",
  "/dashboard?tab=overview#details",
])(
  "sends anonymous visits to %s to login with the destination",
  async (path) => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 401 }))
    )
    const { router } = routerAt(path)
    await router.load()
    expect(router.state.location.pathname).toBe("/login")
    expect(router.state.location.search).toMatchObject({ redirect: path })
    expect(router.state.matches.at(-1)?.routeId).toBe("/login")
  }
)

it("sends signed-in root visits to the dashboard", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation(() => Promise.resolve(Response.json(session)))
  )
  const { router } = routerAt("/")
  await router.load()
  expect(router.state.location.pathname).toBe("/dashboard")
})

it("allows a permitted session to load the dashboard", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation(() => Promise.resolve(Response.json(session)))
  )
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
      (match) => match.routeId === "__root__" && match.status === "error"
    )
  ).toBe(true)
  expect(router.state.matches.at(-1)?.context).not.toHaveProperty("session")
})

it("keeps signed-in non-admins out of the admin dashboard", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockImplementation(() =>
        Promise.resolve(
          Response.json({ ...session, user: { ...session.user, admin: false } })
        )
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

async function renderAt(path: string) {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true)
  const state = routerAt(path)
  const container = document.createElement("div")
  document.body.append(container)
  const root = createRoot(container)
  roots.push(root)
  await act(async () => {
    await state.router.load()
    root.render(
      createElement(
        QueryClientProvider,
        { client: state.queryClient },
        createElement(RouterProvider, { router: state.router })
      )
    )
  })
  return { ...state, container }
}

it.each(["/login", "/status", "/missing"])(
  "renders only login for anonymous visits to %s",
  async (path) => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) =>
        Promise.resolve(
          url === "/api/env"
            ? Response.json({
                version: 1,
                auth: { sso_providers: ["google"] },
              })
            : new Response(null, { status: 401 })
        )
      )
    )
    const { container } = await renderAt(path)
    await vi.waitFor(() =>
      expect(container.textContent).toContain("Continue with Google")
    )
    expect(container.textContent).toContain("Sign in to Nautilus Admin")
    expect(container.querySelector("header")).toBeNull()
    expect(container.querySelector("nav")).toBeNull()
    expect(container.textContent).not.toContain("Service status")
    expect(container.textContent).not.toContain("Page not found")
  }
)

it("removes app content when a mounted session expires", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockImplementation((url: string) =>
        Promise.resolve(
          url === "/api/env"
            ? Response.json({ version: 1, auth: { sso_providers: ["google"] } })
            : Response.json(session)
        )
      )
  )
  const { container, queryClient, router } = await renderAt("/forbidden")
  expect(container.querySelector("nav")).not.toBeNull()
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockImplementation((url: string) =>
        Promise.resolve(
          url === "/api/env"
            ? Response.json({ version: 1, auth: { sso_providers: ["google"] } })
            : new Response(null, { status: 401 })
        )
      )
  )
  await act(async () => {
    await queryClient.refetchQueries(sessionQueryOptions())
  })
  await vi.waitFor(() => expect(router.state.location.pathname).toBe("/login"))
  expect(container.querySelector("nav")).toBeNull()
  expect(container.textContent).not.toContain("Administrator access required")
})

it("fails closed on session errors and retries the session check", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockImplementation(() =>
        Promise.resolve(new Response(null, { status: 503 }))
      )
  )
  const { container } = await renderAt("/dashboard")
  expect(container.textContent).toContain("Unable to check your session")
  expect(container.querySelector("nav")).toBeNull()
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation(() => Promise.resolve(Response.json(session)))
  )
  await act(async () => {
    container.querySelector("button")!.click()
  })
  await vi.waitFor(() => expect(container.querySelector("nav")).not.toBeNull())
  expect(container.textContent).not.toContain("Unable to check your session")
})
