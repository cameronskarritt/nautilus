// @vitest-environment jsdom
import { act } from "react"
import { createRoot } from "react-dom/client"
import { QueryClientProvider } from "@tanstack/react-query"
import {
  createMemoryHistory,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router"
import { afterEach, expect, it, vi } from "vitest"
import { createQueryClient } from "@workspace/api"
import { routeTree } from "./routeTree.gen"

const clients: ReturnType<typeof createQueryClient>[] = []
const roots: ReturnType<typeof createRoot>[] = []
vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true)

afterEach(async () => {
  await act(async () => {
    for (const root of roots) root.unmount()
  })
  for (const client of clients) client.clear()
  roots.length = 0
  clients.length = 0
  document.body.replaceChildren()
  vi.restoreAllMocks()
})

async function renderLogin(path = "/login") {
  vi.spyOn(window, "scrollTo").mockImplementation(() => {})
  const queryClient = createQueryClient()
  clients.push(queryClient)
  const router = createRouter({
    routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: [path] }),
    isServer: false,
  })
  await router.load()
  const container = document.createElement("div")
  document.body.append(container)
  const root = createRoot(container)
  roots.push(root)
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
  })
  return container
}

function mockAuth(providers = ["google"], status = 401) {
  return vi
    .spyOn(globalThis, "fetch")
    .mockImplementation(async (input) =>
      String(input) === "/api/env"
        ? Response.json({ version: 1, auth: { sso_providers: providers } })
        : new Response(null, { status })
    )
}

it("offers Google as the only sign-in method and preserves the destination", async () => {
  mockAuth()
  const container = await renderLogin(
    "/login?redirect=%2Fdashboard%3Ftab%3Doverview%23details"
  )
  await waitFor(() =>
    expect(container.textContent).toContain("Continue with Google")
  )
  const link = container.querySelector('a[href^="/api/auth/sso/google"]')
  expect(link).not.toBeNull()
  const url = new URL(link!.getAttribute("href")!, window.location.origin)
  expect(url.searchParams.get("redirect")).toBe(
    `${window.location.origin}/dashboard?tab=overview#details`
  )
  expect(container.querySelector("input, form")).toBeNull()
  expect(container.querySelector("header, nav")).toBeNull()
  expect(container.textContent).not.toContain("Your workspace starts here")
  expect(
    container.querySelector('a[href*="register"], a[href*="recovery"]')
  ).toBeNull()
})

it("does not expose a sign-in link when Google is unavailable", async () => {
  mockAuth([])
  const container = await renderLogin()
  await waitFor(() =>
    expect(container.textContent).toContain("Google sign-in is not available")
  )
  expect(container.querySelector('a[href^="/api/auth"]')).toBeNull()
  expect(container.querySelector("button")?.textContent).toBe("Try again")
})

it("lets users retry a failed session check before starting Google sign-in", async () => {
  const fetch = mockAuth(["google"], 503)
  const container = await renderLogin()
  await waitFor(() =>
    expect(container.textContent).toContain("We couldn't check your session")
  )
  expect(container.querySelector('a[href^="/api/auth"]')).toBeNull()
  fetch.mockImplementation(async (input) =>
    String(input) === "/api/env"
      ? Response.json({ version: 1, auth: { sso_providers: ["google"] } })
      : new Response(null, { status: 401 })
  )
  await act(async () => container.querySelector("button")!.click())
  await waitFor(() =>
    expect(container.textContent).toContain("Continue with Google")
  )
})

it("shows a safe callback error with a Google retry link", async () => {
  mockAuth()
  const container = await renderLogin("/login?error=AUTH-1&message=untrusted")
  await waitFor(() =>
    expect(container.textContent).toContain("Continue with Google")
  )
  expect(container.querySelector('[role="alert"]')?.textContent).toBe(
    "Google sign-in could not be completed. Please try again."
  )
  expect(container.textContent).not.toContain("untrusted")
})

async function waitFor(assertion: () => void) {
  await vi.waitFor(async () => {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    assertion()
  })
}

it.each(["/", "/status", "/missing-page"])(
  "shows only login for a signed-out visit to %s",
  async (path) => {
    mockAuth()
    const container = await renderLogin(path)
    await waitFor(() =>
      expect(container.textContent).toContain("Continue with Google")
    )
    expect(container.querySelector("header, nav")).toBeNull()
    expect(container.textContent).not.toContain("Page not found")
    expect(container.textContent).not.toContain("Nautilus API")
  }
)
