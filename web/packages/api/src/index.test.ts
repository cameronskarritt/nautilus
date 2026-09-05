import { afterEach, describe, expect, it, vi } from "vitest"

import { createQueryClient, healthQueryOptions } from "./index"

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe("healthQueryOptions", () => {
  it("reads the backend health contract", async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValue(Response.json({ status: "ok" }))
    vi.stubGlobal("fetch", fetchMock)

    const client = createQueryClient()
    await expect(client.fetchQuery(healthQueryOptions())).resolves.toEqual({
      status: "ok",
    })
  })

  it("rejects a shutdown response with an unsuccessful HTTP status", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn<typeof fetch>()
        .mockResolvedValue(
          Response.json({ status: "shutting_down" }, { status: 503 })
        )
    )

    const client = createQueryClient()
    await expect(
      client.fetchQuery({ ...healthQueryOptions(), retry: false })
    ).rejects.toThrow("Health request failed (503)")
  })

  it.each([null, {}, { status: "unknown" }, { status: 1 }])(
    "rejects an invalid response: %j",
    async (body) => {
      vi.stubGlobal(
        "fetch",
        vi.fn<typeof fetch>().mockResolvedValue(Response.json(body))
      )

      const client = createQueryClient()
      await expect(
        client.fetchQuery({ ...healthQueryOptions(), retry: false })
      ).rejects.toThrow("Invalid health response")
    }
  )

  it("forwards query cancellation and same-origin credentials to fetch", async () => {
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
    const request = client.fetchQuery(healthQueryOptions())
    const rejection = expect(request).rejects.toThrow()

    expect(fetchMock).toHaveBeenCalledWith("/api/health", {
      signal: expect.any(AbortSignal),
      credentials: "same-origin",
    })
    const signal = fetchMock.mock.calls[0]?.[1]?.signal
    expect(signal?.aborted).toBe(false)

    await client.cancelQueries({ queryKey: ["health"] })

    expect(signal?.aborted).toBe(true)
    await rejection
  })
})
