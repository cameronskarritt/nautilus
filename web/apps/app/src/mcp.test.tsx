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
import type { SessionResponse } from "@workspace/models"
import { routeTree } from "./routeTree.gen"
import { parseSearch, stringifySearch } from "./search"

const session: SessionResponse = {
  user: {
    id: "session-user",
    email: "person@example.com",
    auth_provider: "google",
    verified: true,
    admin: false,
    mfa_enabled: false,
    created_at: "2026-09-05T00:00:00Z",
  },
  organization: null,
  assumed: false,
  flags: {},
}
const consent = {
  client_name: '<img src=x onerror="alert(1)"> Assistant',
  redirect_uri: "https://client.example.com/callback",
  scope: "read write",
  user_id: "reviewed-user",
  organization: { id: "reviewed-org", name: "<script>Team</script>" },
}
const oauth = {
  client_id: "mcp_client_test",
  redirect_uri: consent.redirect_uri,
  response_type: "code",
  scope: "read write",
  state: "9007199254740993",
  code_challenge: "A".repeat(43),
  code_challenge_method: "S256",
  resource: "https://mcp.example.com/mcp",
}
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

function mockConsent({ signedIn = true, requestStatus = 200 } = {}) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    if (url === "/api/users/me")
      return signedIn
        ? Response.json(session)
        : new Response(null, { status: 401 })
    if (url === "/api/env")
      return Response.json({ version: 1, auth: { sso_providers: ["google"] } })
    if (url.startsWith("/api/mcp/oauth/request?"))
      return Response.json(consent, { status: requestStatus })
    if (url === "/api/mcp/oauth/authorize")
      return new Response(null, { status: 409 })
    throw new Error(`Unexpected request: ${url}`)
  })
}

async function renderConsent(state = oauth.state) {
  vi.spyOn(window, "scrollTo").mockImplementation(() => {})
  const path = `/mcp/authorize?${new URLSearchParams({ ...oauth, state })}`
  const queryClient = createQueryClient()
  clients.push(queryClient)
  const router = createRouter({
    parseSearch,
    stringifySearch,
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
  return { container, router, path }
}

async function waitFor(assertion: () => void) {
  await vi.waitFor(async () => {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    assertion()
  })
}

it("shows the reviewed app, organization, and scopes without granting access automatically", async () => {
  const fetch = mockConsent()
  const { container } = await renderConsent()
  await waitFor(() =>
    expect(container.textContent).toContain(consent.client_name)
  )
  expect(container.textContent).toContain(consent.organization.name)
  expect(container.textContent).toContain(
    "Read your organization's data through MCP."
  )
  expect(container.textContent).toContain(
    "Create and update your organization's data through MCP."
  )
  expect(container.textContent).toContain(
    "You will return to client.example.com."
  )
  expect(container.querySelector("img, script")).toBeNull()
  expect(fetch.mock.calls.some(([, init]) => init?.method === "POST")).toBe(
    false
  )
  const request = fetch.mock.calls.find(([input]) =>
    String(input).startsWith("/api/mcp/oauth/request?")
  )
  expect(request).toBeDefined()
  expect(
    Object.fromEntries(
      new URL(String(request![0]), window.location.origin).searchParams
    )
  ).toEqual(oauth)
})

it.each([
  { label: "Allow connection", decision: "allow", state: "9007199254740993" },
  { label: "Deny", decision: "deny", state: "true" },
])(
  "posts reviewed identity and opaque state only after $label is clicked",
  async ({ label, decision, state }) => {
    const fetch = mockConsent()
    const { container, router, path } = await renderConsent(state)
    await waitFor(() =>
      expect(container.textContent).toContain(consent.client_name)
    )
    expect(fetch.mock.calls.some(([, init]) => init?.method === "POST")).toBe(
      false
    )
    const button = [...container.querySelectorAll("button")].find(
      (element) => element.textContent === label
    )
    expect(button).toBeDefined()
    const location = window.location.href
    await act(async () => button!.click())
    await waitFor(() =>
      expect(container.querySelector('[role="alert"]')?.textContent).toBe(
        "The connection could not be authorized. Reload and try again."
      )
    )
    const posts = fetch.mock.calls.filter(([, init]) => init?.method === "POST")
    expect(posts).toHaveLength(1)
    expect(posts[0]![0]).toBe("/api/mcp/oauth/authorize")
    expect(posts[0]![1]?.credentials).toBe("same-origin")
    expect(JSON.parse(String(posts[0]![1]?.body))).toEqual({
      ...oauth,
      state,
      decision,
      user_id: consent.user_id,
      organization_id: consent.organization.id,
    })
    expect(window.location.href).toBe(location)
    expect(router.state.location.href).toBe(path)
  }
)

it("shows invalid requests without consent actions or automatic authorization", async () => {
  const fetch = mockConsent({ requestStatus: 400 })
  const { container, router, path } = await renderConsent()
  await waitFor(() =>
    expect(container.querySelector('[role="alert"]')?.textContent).toBe(
      "This connection request is not valid."
    )
  )
  expect(container.textContent).not.toContain("Allow connection")
  expect(container.textContent).not.toContain(consent.client_name)
  expect(fetch.mock.calls.some(([, init]) => init?.method === "POST")).toBe(
    false
  )
  expect(router.state.location.href).toBe(path)
})

it("sends signed-out users to Google login with the complete consent destination", async () => {
  const fetch = mockConsent({ signedIn: false })
  const { container, path } = await renderConsent()
  await waitFor(() =>
    expect(container.textContent).toContain("Continue with Google")
  )
  const link = container.querySelector('a[href^="/api/auth/sso/google"]')
  expect(link).not.toBeNull()
  const url = new URL(link!.getAttribute("href")!, window.location.origin)
  expect(url.searchParams.get("redirect")).toBe(
    `${window.location.origin}${path}`
  )
  expect(
    fetch.mock.calls.some(([input]) =>
      String(input).startsWith("/api/mcp/oauth/")
    )
  ).toBe(false)
})
