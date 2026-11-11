// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi, type Mock } from "vitest";
import {
  ApiError,
  apiFetch,
  buildUrl,
  deleteDownload,
  downloadResource,
  errorMessage,
  fetchDownloaderItems,
  fetchDownloaders,
  fetchIndexerCategories,
  fetchIndexerResources,
  fetchIndexers,
  isAuthenticated,
  logout,
  organizeDownload,
} from "./api";

const KEY = "autoget_auth";
const LOCK_KEY = "autoget_auth_refresh_lock";

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
  Object.defineProperty(document, "visibilityState", { value: "visible", configurable: true });
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

  it("refreshes once for concurrent 401s in the same tab", async () => {
    localStorage.setItem(KEY, JSON.stringify({ access_token: "old", refresh_token: "r1" }));
    const mock = vi.fn<typeof fetch>(async (input, init) => {
      const url = String(input);
      if (url === "/auth/refresh") {
        return res(200, {
          access_token: "new",
          refresh_token: "r2",
          expires_in: 300,
          token_type: "Bearer",
        });
      }
      const auth = new Headers(init?.headers).get("Authorization");
      return auth === "Bearer new" ? res(200, { ok: true }) : res(401);
    });
    vi.stubGlobal("fetch", mock);

    const responses = await Promise.all([
      apiFetch("/api/v1/a"),
      apiFetch("/api/v1/b"),
      apiFetch("/api/v1/c"),
    ]);

    expect(responses.map((response) => response.status)).toEqual([200, 200, 200]);
    const refreshCalls = mock.mock.calls.filter((call) => String(call[0]) === "/auth/refresh");
    expect(refreshCalls).toHaveLength(1);
  });

  it("waits for another tab's refresh lock instead of refreshing concurrently", async () => {
    const LOCK_KEY = "autoget_auth_refresh_lock";
    localStorage.setItem(KEY, JSON.stringify({ access_token: "old", refresh_token: "r1" }));
    // Another tab already holds the refresh lock.
    localStorage.setItem(LOCK_KEY, JSON.stringify({ id: "other-tab", ts: Date.now() }));
    const mock = vi.fn<typeof fetch>(async (input, init) => {
      const url = String(input);
      if (url === "/auth/refresh") {
        return res(200, {
          access_token: "new",
          refresh_token: "r2",
          expires_in: 300,
          token_type: "Bearer",
        });
      }
      const auth = new Headers(init?.headers).get("Authorization");
      return auth === "Bearer new" ? res(200, { ok: true }) : res(401);
    });
    vi.stubGlobal("fetch", mock);

    const pending = apiFetch("/api/v1/indexers");
    // The holder finishes and releases the lock shortly after.
    setTimeout(() => localStorage.removeItem(LOCK_KEY), 120);

    const response = await pending;

    expect(response.status).toBe(200);
    const refreshCalls = mock.mock.calls.filter((call) => String(call[0]) === "/auth/refresh");
    expect(refreshCalls).toHaveLength(1);
  });

  it("redirects to login when the refresh token is rejected", async () => {
    localStorage.setItem(KEY, JSON.stringify({ access_token: "old", refresh_token: "r1" }));
    apiResponses = [401];
    refreshResponse = () => res(401, { error: "refresh failed" });
    installFetchMock();

    void apiFetch("/api/v1/indexers");

    await vi.waitFor(() => expect(window.location.href).toContain("/auth/login?redirect="), {
      timeout: 5000,
    });
  });

  it("adopts another tab's session when it loses the refresh race", async () => {
    localStorage.setItem(KEY, JSON.stringify({ access_token: "old", refresh_token: "r1" }));
    const mock = vi.fn<typeof fetch>(async (input, init) => {
      const url = String(input);
      if (url === "/auth/refresh") {
        // A concurrent tab exchanged the single-use refresh token first and
        // published its session before our attempt was rejected as a replay.
        localStorage.setItem(
          KEY,
          JSON.stringify({
            access_token: "new",
            refresh_token: "r2",
            expires_in: 300,
            token_type: "Bearer",
          }),
        );
        return res(401, { error: "invalid_grant" });
      }
      const auth = new Headers(init?.headers).get("Authorization");
      return auth === "Bearer new" ? res(200, { ok: true }) : res(401);
    });
    vi.stubGlobal("fetch", mock);

    const response = await apiFetch("/api/v1/indexers");

    expect(response.status).toBe(200);
    expect(window.location.href).not.toContain("/auth/login");
  });

  it("waits for the winning tab to publish its session", async () => {
    localStorage.setItem(KEY, JSON.stringify({ access_token: "old", refresh_token: "r1" }));
    const mock = vi.fn<typeof fetch>(async (input, init) => {
      const url = String(input);
      if (url === "/auth/refresh") {
        return res(401, { error: "invalid_grant" });
      }
      const auth = new Headers(init?.headers).get("Authorization");
      return auth === "Bearer new" ? res(200, { ok: true }) : res(401);
    });
    vi.stubGlobal("fetch", mock);

    const pending = apiFetch("/api/v1/indexers");
    // The winning tab publishes its session a moment after our rejection.
    setTimeout(() => {
      localStorage.setItem(
        KEY,
        JSON.stringify({
          access_token: "new",
          refresh_token: "r2",
          expires_in: 300,
          token_type: "Bearer",
        }),
      );
      window.dispatchEvent(new StorageEvent("storage", { key: KEY }));
    }, 50);

    const response = await pending;

    expect(response.status).toBe(200);
    expect(window.location.href).not.toContain("/auth/login");
  });

  it("defers redirect to login until the tab becomes visible", async () => {
    Object.defineProperty(document, "visibilityState", { value: "hidden", configurable: true });
    localStorage.setItem(KEY, JSON.stringify({ access_token: "old", refresh_token: "r1" }));
    apiResponses = [401];
    refreshResponse = () => res(401, { error: "refresh failed" });
    installFetchMock();

    const pending = apiFetch("/api/v1/indexers");

    // Wait past SESSION_HANDOFF_TIMEOUT_MS to prove the tab did not redirect while hidden
    const outcome = await Promise.race([
      pending.then(() => "resolved"),
      new Promise((resolve) => setTimeout(() => resolve("pending"), 3000)),
    ]);
    expect(outcome).toBe("pending");
    expect(window.location.href).not.toContain("/auth/login");

    // Now user switches back to the tab
    Object.defineProperty(document, "visibilityState", { value: "visible", configurable: true });
    document.dispatchEvent(new Event("visibilitychange"));

    expect(window.location.href).toContain("/auth/login?redirect=");
  });

  it("does not redirect visible tab if another tab refreshed the session after handoff timeout", async () => {
    Object.defineProperty(document, "visibilityState", { value: "hidden", configurable: true });
    localStorage.setItem(KEY, JSON.stringify({ access_token: "old", refresh_token: "r1" }));
    apiResponses = [401];
    refreshResponse = () => res(401, { error: "refresh failed" });
    installFetchMock();

    void apiFetch("/api/v1/indexers");

    // Wait past handoff timeout so the deferred visibility listener is registered
    await new Promise((resolve) => setTimeout(resolve, 3000));
    expect(window.location.href).not.toContain("/auth/login");

    // Meanwhile another tab completes refresh late and writes new tokens
    localStorage.setItem(
      KEY,
      JSON.stringify({
        access_token: "recovered",
        refresh_token: "r2",
        expires_in: 300,
        token_type: "Bearer",
      }),
    );

    // User switches back to this tab
    Object.defineProperty(document, "visibilityState", { value: "visible", configurable: true });
    document.dispatchEvent(new Event("visibilitychange"));

    expect(window.location.href).not.toContain("/auth/login");
  });

  it("adopts another tab's session broadcasted over BroadcastChannel", async () => {
    localStorage.setItem(KEY, JSON.stringify({ access_token: "old", refresh_token: "r1" }));
    const mock = vi.fn<typeof fetch>(async (input, init) => {
      const url = String(input);
      if (url === "/auth/refresh") {
        return res(401, { error: "invalid_grant" });
      }
      const auth = new Headers(init?.headers).get("Authorization");
      return auth === "Bearer new_bc" ? res(200, { ok: true }) : res(401);
    });
    vi.stubGlobal("fetch", mock);

    const pending = apiFetch("/api/v1/indexers");

    // Another tab finishes refresh and broadcasts session_updated
    setTimeout(() => {
      localStorage.setItem(
        KEY,
        JSON.stringify({
          access_token: "new_bc",
          refresh_token: "r2",
          expires_in: 300,
          token_type: "Bearer",
        }),
      );
      const channel = new BroadcastChannel("autoget_auth_channel");
      channel.postMessage({ type: "session_updated" });
      channel.close();
    }, 50);

    const response = await pending;

    expect(response.status).toBe(200);
    expect(window.location.href).not.toContain("/auth/login");
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

  it("does not spin the lock deadline when localStorage writes throw", async () => {
    localStorage.setItem(KEY, JSON.stringify({ access_token: "old", refresh_token: "r1" }));
    apiResponses = [401];
    refreshResponse = () => res(200, { access_token: "new", refresh_token: "r2" });
    installFetchMock();

    const origSetItem = localStorage.setItem.bind(localStorage);
    // Simulate quota exceeded or storage blocked on writing the lock key
    localStorage.setItem = vi.fn<typeof localStorage.setItem>((key: string, value: string) => {
      if (key === LOCK_KEY) {
        throw new DOMException("QuotaExceededError", "QuotaExceededError");
      }
      origSetItem(key, value);
    });

    const start = Date.now();
    const response = await apiFetch("/api/v1/indexers");
    const elapsed = Date.now() - start;

    expect(response.status).toBe(200);
    // Should complete immediately via in-page single flight, not waiting for the 10s tab lock deadline
    expect(elapsed).toBeLessThan(1000);
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

describe("request building and error handling", () => {
  beforeEach(() => {
    localStorage.clear();
    apiResponses = [];
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function urlOf(mock: Mock<typeof fetch>, call = 0): URL {
    return new URL(String(mock.mock.calls[call][0]), "http://x");
  }

  it("encodes keyword and category as query parameters", async () => {
    const mock = vi.fn<typeof fetch>(async () => res(200, { pagination: {}, resources: [] }));
    vi.stubGlobal("fetch", mock);
    const keyword = "a&pageSize=1#x+%";
    await fetchIndexerResources("idx", "c&d", keyword, 2);
    const url = urlOf(mock);
    expect(url.searchParams.get("keyword")).toBe(keyword);
    expect(url.searchParams.get("category")).toBe("c&d");
    expect(url.searchParams.get("pageSize")).toBe("100");
    expect(url.searchParams.get("page")).toBe("2");
  });

  it("encodes path segments and the organize hint", async () => {
    const mock = vi.fn<typeof fetch>(async () => res(200, {}));
    vi.stubGlobal("fetch", mock);
    await fetchDownloaderItems("a/b c", "failed");
    expect(String(mock.mock.calls[0][0])).toContain("/downloaders/a%2Fb%20c?state=failed");
    await deleteDownload("x/y");
    expect(String(mock.mock.calls[1][0])).toBe("/api/v1/download/x%2Fy");
    await organizeDownload("id", "re_plan", "a&b=c");
    expect(urlOf(mock, 2).searchParams.get("user_hint")).toBe("a&b=c");
    expect(urlOf(mock, 2).searchParams.get("action")).toBe("re_plan");
  });

  it("omits empty query values", () => {
    expect(buildUrl("/p", { a: "", b: undefined, c: 1 })).toBe("/p?c=1");
  });

  it("rejects with ApiError on HTTP failure but resolves an empty list", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(async () => res(500)),
    );
    await expect(fetchIndexers()).rejects.toMatchObject({ name: "ApiError", status: 500 });
    await expect(fetchDownloaders()).rejects.toBeInstanceOf(ApiError);
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(async () => res(200, [])),
    );
    await expect(fetchIndexers()).resolves.toEqual([]);
  });

  it("wraps network failures in ApiError", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(async () => {
        throw new TypeError("boom");
      }),
    );
    await expect(fetchIndexerCategories("a")).rejects.toBeInstanceOf(ApiError);
  });

  it("surfaces 409 from downloadResource and normalises downloader items", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(async () => res(409)),
    );
    await expect(downloadResource("i", "r")).rejects.toMatchObject({ status: 409 });
    expect(errorMessage(new ApiError("x", 409))).toBe("Already exists.");
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(async () => res(200, {})),
    );
    const items = await fetchDownloaderItems("d", "failed");
    expect(items.resources).toEqual([]);
    expect(items.state.count_of_failed).toBe(0);
  });
});
