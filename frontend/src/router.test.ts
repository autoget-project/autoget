// @vitest-environment jsdom
import "urlpattern-polyfill";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./utils/api.ts", () => ({
  fetchIndexers: vi.fn<() => Promise<string[]>>(),
  fetchIndexerCategories: vi.fn<(id: string) => Promise<unknown[]>>(),
  fetchDownloaders: vi.fn<() => Promise<unknown[]>>().mockResolvedValue([]),
}));
vi.mock("./views/search_view", () => ({}));
vi.mock("./views/indexer_view", () => ({}));
vi.mock("./views/downloader_view", () => ({}));
vi.mock("./views/404_view", () => ({}));

import * as api from "./utils/api.ts";
import "./router.ts";

async function mount(path: string) {
  window.history.pushState({}, "", path);
  const el = document.createElement("app-router") as HTMLElement & {
    updateComplete: Promise<boolean>;
  };
  document.body.appendChild(el);
  await el.updateComplete;
  return el;
}

describe("app-router", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
    vi.mocked(api.fetchIndexers).mockReset();
    vi.mocked(api.fetchIndexerCategories).mockReset();
  });

  it("shows an error instead of redirecting to /indexers/undefined when no indexers", async () => {
    vi.mocked(api.fetchIndexers).mockResolvedValue([]);
    const el = await mount("/");
    await vi.waitFor(() => expect(el.shadowRoot?.textContent).toContain("No indexers available"), {
      timeout: 5000,
    });
    expect(window.location.pathname).not.toContain("undefined");
    // connectedCallback fetch + at most 3 bounded attempts
    expect(vi.mocked(api.fetchIndexers).mock.calls.length).toBeLessThanOrEqual(4);
  });

  it("goes to /404 when categories are empty", async () => {
    vi.mocked(api.fetchIndexers).mockResolvedValue(["a"]);
    vi.mocked(api.fetchIndexerCategories).mockResolvedValue([]);
    const el = await mount("/indexers/a");
    await vi.waitFor(() => expect(el.shadowRoot?.querySelector("not-found-view")).not.toBeNull());
  });

  it("redirects to first category on happy path", async () => {
    vi.mocked(api.fetchIndexers).mockResolvedValue(["a"]);
    vi.mocked(api.fetchIndexerCategories).mockResolvedValue([
      { id: "c", name: "C", subCategories: [] } as never,
    ]);
    await mount("/");
    await vi.waitFor(() => expect(window.location.pathname).toBe("/indexers/a/c"));
  });
});
