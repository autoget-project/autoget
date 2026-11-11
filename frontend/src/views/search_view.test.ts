// @vitest-environment jsdom
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";

import type { Category } from "../utils/api";

vi.mock("../utils/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../utils/api")>()),
  fetchIndexers: vi.fn<() => Promise<string[]>>(),
  fetchIndexerCategories: vi.fn<(indexer: string) => Promise<Category[]>>(),
}));
vi.mock("../components/navbar.ts", () => ({}));
vi.mock("../components/resource_list.ts", () => ({}));

import * as api from "../utils/api";
import "./search_view.ts";

type View = HTMLElement & { updateComplete: Promise<boolean> };

const fetchIndexers = vi.mocked(api.fetchIndexers);
const fetchCategories = vi.mocked(api.fetchIndexerCategories);

async function settle(el: View): Promise<void> {
  for (let i = 0; i < 4; i++) {
    await Promise.resolve();
    await el.updateComplete;
  }
}

async function mount(url = "/search"): Promise<View> {
  window.history.pushState({}, "", url);
  const el = document.createElement("search-view") as View;
  document.body.appendChild(el);
  await settle(el);
  return el;
}

describe("search-view", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
    fetchIndexers.mockReset();
    fetchCategories.mockReset().mockResolvedValue([]);
  });

  afterEach(() => {
    document.body.innerHTML = "";
    vi.restoreAllMocks();
  });

  it("lists indexers and does not render results without a query", async () => {
    fetchIndexers.mockResolvedValue(["mteam", "hdsky"]);
    const el = await mount();
    expect(el.shadowRoot?.textContent).toContain("mteam");
    expect(el.shadowRoot?.textContent).toContain("hdsky");
    expect(el.shadowRoot?.querySelector("resource-list")).toBeNull();
    expect(el.shadowRoot?.querySelector('[role="alert"]')).toBeNull();
  });

  it("renders resource-list with URL params", async () => {
    fetchIndexers.mockResolvedValue(["mteam"]);
    const el = await mount("/search?keyword=matrix&indexer=mteam&page=2");
    const list = el.shadowRoot?.querySelector("resource-list") as unknown as {
      keyword: string;
      indexerId: string;
      page: number;
    };
    expect(list.keyword).toBe("matrix");
    expect(list.indexerId).toBe("mteam");
    expect(list.page).toBe(2);
    expect(fetchCategories).toHaveBeenCalledWith("mteam");
    expect(el.shadowRoot?.textContent).toContain('Result For "matrix"');
  });

  it("shows an error with Retry when indexers fail to load", async () => {
    fetchIndexers.mockRejectedValueOnce(new Error("offline"));
    const el = await mount();
    const alert = el.shadowRoot?.querySelector('[role="alert"]');
    expect(alert?.textContent).toContain("Failed to load indexers: offline");

    fetchIndexers.mockResolvedValueOnce(["mteam"]);
    alert?.querySelector<HTMLButtonElement>("button")?.click();
    await settle(el);
    expect(el.shadowRoot?.querySelector('[role="alert"]')).toBeNull();
    expect(el.shadowRoot?.textContent).toContain("mteam");
  });

  it("stops listening to popstate once disconnected", async () => {
    fetchIndexers.mockResolvedValue(["mteam"]);
    const removeSpy = vi.spyOn(window, "removeEventListener");
    const el = await mount();
    el.remove();
    expect(removeSpy).toHaveBeenCalledWith("popstate", expect.any(Function));
  });
});
