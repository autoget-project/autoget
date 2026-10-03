// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi, type Mock } from "vitest";
import { apiFetch, isAuthenticated, logout } from "./api";

const KEY = "autoget_auth";

function res(status: number, body: unknown = {}): Response {
  return {
    status,
    ok: status >= 200 && status < 300,
    json: async () => body,
  } as Response;
}

let apiResponses: number[] = [];
let refreshResponse: () => Response = () => res(200);

function installFetchMock(): Mock<typeof fetch> {
  const mock = vi.fn<typeof fetch>(async (input) => {
    const url = String(input);
    if (url === "/auth/refresh") {
      return refreshResponse();
    }
    return res(apiResponses.shift() ?? 200);
  });
  vi.stubGlobal("fetch", mock);
  return mock;
}

let storage: Record<string, string> = {};

function installStorage(): void {
  storage = {};
  const mock = {
    getItem: (key: string) => storage[key] ?? null,
    setItem: (key: string, value: string) => {
      storage[key] = value;
    },
    removeItem: (key: string) => {
      delete storage[key];
    },
    clear: () => {
      storage = {};
    },
  };
  Object.defineProperty(window, "localStorage", {
    value: mock,
    configurable: true,
    writable: true,
  });
  Object.defineProperty(globalThis, "localStorage", {
    value: mock,
    configurable: true,
    writable: true,
  });
}

beforeEach(() => {
  apiResponses = [];
  refreshResponse = () => res(200);
  installStorage();
  // jsdom has no Web Locks; the module falls back to an in-page single flight.
  Object.defineProperty(navigator, "locks", { value: undefined, configurable: true });
  Object.defineProperty(window, "location", {
    value: { href: "http://localhost/indexers/mteam" },
    writable: true,
    configurable: true,
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("apiFetch auth handling", () => {
  it("attaches the stored access token", async () => {
    localStorage.setItem(KEY, JSON.stringify({ access_token: "tok", refresh_token: "r1" }));
    apiResponses = [200];
    const mock = installFetchMock();

    await apiFetch("/api/v1/indexers");

    const init = mock.mock.calls[0][1];
    expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer tok");
  });

  it("refreshes and retries once on 401", async () => {
    localStorage.setItem(KEY, JSON.stringify({ access_token: "old", refresh_token: "r1" }));
    apiResponses = [401, 200];
    refreshResponse = () =>
      res(200, { access_token: "new", refresh_token: "r2", expires_in: 300, token_type: "Bearer" });
    installFetchMock();

    const response = await apiFetch("/api/v1/indexers");

    expect(response.status).toBe(200);
    expect(JSON.parse(localStorage.getItem(KEY) ?? "{}").access_token).toBe("new");
  });

  it("redirects to login when the refresh token is rejected", async () => {
    localStorage.setItem(KEY, JSON.stringify({ access_token: "old", refresh_token: "r1" }));
    apiResponses = [401];
    refreshResponse = () => res(401, { error: "refresh failed" });
    installFetchMock();

    void apiFetch("/api/v1/indexers");

    await vi.waitFor(() => expect(window.location.href).toContain("/auth/login?redirect="));
  });

  it("redirects to login when no session is stored", async () => {
    apiResponses = [401];
    installFetchMock();

    void apiFetch("/api/v1/indexers");

    await vi.waitFor(() => expect(window.location.href).toContain("/auth/login"));
  });

  it("redirects to the denial page on 403 without trying to refresh", async () => {
    localStorage.setItem(KEY, JSON.stringify({ access_token: "old", refresh_token: "r1" }));
    apiResponses = [403];
    const mock = installFetchMock();

    void apiFetch("/api/v1/indexers");

    await vi.waitFor(() => expect(window.location.href).toContain("/auth/denied"));
    expect(mock.mock.calls.some((call) => String(call[0]) === "/auth/refresh")).toBe(false);
  });

  it("keeps the session and returns the response when the provider is unavailable", async () => {
    localStorage.setItem(KEY, JSON.stringify({ access_token: "old", refresh_token: "r1" }));
    apiResponses = [401];
    refreshResponse = () => res(503, { error: "auth unavailable" });
    installFetchMock();
    const hrefBefore = window.location.href;

    const response = await apiFetch("/api/v1/indexers");

    expect(response.status).toBe(401);
    expect(window.location.href).toBe(hrefBefore);
  });

  it("keeps the session when the refresh request fails on the network", async () => {
    localStorage.setItem(KEY, JSON.stringify({ access_token: "old", refresh_token: "r1" }));
    const mock = vi.fn<typeof fetch>(async (input) => {
      if (String(input) === "/auth/refresh") {
        throw new TypeError("network error");
      }
      return res(401);
    });
    vi.stubGlobal("fetch", mock);
    const hrefBefore = window.location.href;

    const response = await apiFetch("/api/v1/indexers");

    expect(response.status).toBe(401);
    expect(window.location.href).toBe(hrefBefore);
  });
});

describe("session helpers", () => {
  it("reports whether a session is stored", () => {
    expect(isAuthenticated()).toBe(false);
    localStorage.setItem(KEY, JSON.stringify({ access_token: "tok" }));
    expect(isAuthenticated()).toBe(true);
  });

  it("clears the stored session on logout", () => {
    localStorage.setItem(KEY, JSON.stringify({ access_token: "tok" }));
    logout();
    expect(localStorage.getItem(KEY)).toBeNull();
    expect(window.location.href).toBe("/");
  });
});
