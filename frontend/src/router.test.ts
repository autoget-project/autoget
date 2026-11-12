// @vitest-environment jsdom
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

  it("loads an indexer whose name needs URL encoding (m-team:adult)", async () => {
    vi.mocked(api.fetchIndexers).mockResolvedValue(["m-team", "m-team:adult"]);
    vi.mocked(api.fetchIndexerCategories).mockResolvedValue([
      { id: "c", name: "C", subCategories: [] } as never,
    ]);
    const el = await mount("/indexers/m-team%3Aadult");
    await vi.waitFor(() => expect(window.location.pathname).toBe("/indexers/m-team%3Aadult/c"));
    expect(vi.mocked(api.fetchIndexerCategories)).toHaveBeenCalledWith("m-team:adult");
    await vi.waitFor(() => expect(el.shadowRoot?.querySelector("indexer-view")).not.toBeNull());
    const rendered = el.shadowRoot?.querySelector("indexer-view") as Element & {
      indexerId?: string;
    };
    expect(rendered.indexerId).toBe("m-team:adult");
    expect(el.shadowRoot?.querySelector("not-found-view")).toBeNull();
  });

  it("shows an error with retry when the categories fetch fails", async () => {
    vi.mocked(api.fetchIndexers).mockResolvedValue(["a"]);
    vi.mocked(api.fetchIndexerCategories).mockRejectedValue(new Error("boom"));
    const el = await mount("/indexers/a");
    await vi.waitFor(() =>
      expect(el.shadowRoot?.textContent).toContain("Failed to load categories"),
    );
    expect(el.shadowRoot?.querySelector("not-found-view")).toBeNull();
    expect(el.shadowRoot?.querySelector("button")).not.toBeNull();
  });

  it("does not leak a rejection when the indexers fetch throws", async () => {
    vi.mocked(api.fetchIndexers).mockRejectedValue(new Error("boom"));
    const el = await mount("/");
    await vi.waitFor(() => expect(el.shadowRoot?.textContent).toContain("No indexers available"), {
      timeout: 5000,
    });
  });
});
