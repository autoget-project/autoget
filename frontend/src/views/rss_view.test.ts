// @vitest-environment jsdom
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";

vi.mock("../utils/api.ts", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../utils/api.ts")>()),
  fetchSearches: vi.fn<() => Promise<api.RSSSearch[]>>(),
  fetchIndexers: vi.fn<() => Promise<string[]>>(),
  registerSearch: vi.fn<(i: string, t: string, a: api.RSSSearch["action"]) => Promise<void>>(),
  deleteSearch: vi.fn<(id: number) => Promise<void>>(),
}));
vi.mock("../components/navbar.ts", () => ({}));

import * as api from "../utils/api.ts";
import "./rss_view.ts";

type View = HTMLElement & { updateComplete: Promise<boolean> };

const fetchSearches = vi.mocked(api.fetchSearches);
const fetchIndexers = vi.mocked(api.fetchIndexers);
const registerSearch = vi.mocked(api.registerSearch);
const deleteSearch = vi.mocked(api.deleteSearch);

function search(over: Partial<api.RSSSearch> = {}): api.RSSSearch {
  return {
    id: 1,
    indexer: "mteam",
    text: "some show",
    action: "download",
    created_at: "2024-01-01T00:00:00Z",
    ...over,
  };
}

async function settle(el: View): Promise<void> {
  for (let i = 0; i < 6; i++) {
    await Promise.resolve();
    await el.updateComplete;
  }
}

async function mount(): Promise<View> {
  const el = document.createElement("rss-view") as View;
  document.body.appendChild(el);
  await settle(el);
  return el;
}

describe("rss-view", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
    fetchSearches.mockReset().mockResolvedValue([]);
    fetchIndexers.mockReset().mockResolvedValue(["mteam", "nyaa"]);
    registerSearch.mockReset().mockResolvedValue();
    deleteSearch.mockReset().mockResolvedValue();
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  afterEach(() => {
    document.body.innerHTML = "";
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("shows the empty state", async () => {
    const el = await mount();
    expect(el.shadowRoot?.textContent).toContain("No RSS searches yet.");
  });

  it("renders waiting and matched searches", async () => {
    fetchSearches.mockResolvedValue([
      search(),
      search({ id: 2, action: "notification", res_id: "9", title: "Found It", url: "http://x" }),
    ]);
    const el = await mount();
    const rows = el.shadowRoot?.querySelectorAll("tbody tr");
    expect(rows?.length).toBe(2);
    expect(rows?.[0].textContent).toContain("Waiting");
    expect(rows?.[1].textContent).toContain("Matched");
    expect(rows?.[1].textContent).toContain("Found It");
    expect(el.shadowRoot?.querySelector("a[href='http://x']")).toBeNull();
  });

  it("adds a search then reloads the list", async () => {
    const el = await mount();
    fetchSearches.mockResolvedValue([search({ text: "new thing" })]);
    const input = el.shadowRoot?.querySelector<HTMLInputElement>("input[name='text']");
    input!.value = "new thing";
    el.shadowRoot?.querySelector("form")?.dispatchEvent(new Event("submit", { cancelable: true }));
    await settle(el);
    expect(registerSearch).toHaveBeenCalledWith("mteam", "new thing", "download");
    expect(fetchSearches).toHaveBeenCalledTimes(2);
    expect(el.shadowRoot?.textContent).toContain("new thing");
  });

  it("confirms before deleting and sends a single request", async () => {
    fetchSearches.mockResolvedValue([search()]);
    const confirmFn = vi
      .fn<(m?: string) => boolean>()
      .mockReturnValueOnce(false)
      .mockReturnValue(true);
    vi.stubGlobal("confirm", confirmFn);
    const el = await mount();
    const btn = () => el.shadowRoot?.querySelector<HTMLButtonElement>("button.text-error");
    btn()?.click();
    await settle(el);
    expect(deleteSearch).not.toHaveBeenCalled();

    fetchSearches.mockResolvedValue([]);
    btn()?.click();
    btn()?.click();
    await settle(el);
    expect(deleteSearch).toHaveBeenCalledTimes(1);
    expect(deleteSearch).toHaveBeenCalledWith(1);
    expect(el.shadowRoot?.textContent).toContain("No RSS searches yet.");
  });

  it("shows an error with retry when loading fails", async () => {
    fetchSearches.mockRejectedValueOnce(new api.ApiError("boom"));
    const el = await mount();
    expect(el.shadowRoot?.querySelector("[role='alert']")?.textContent).toContain("boom");
    const retry = [...(el.shadowRoot?.querySelectorAll("button") ?? [])].find(
      (b) => b.textContent === "Retry",
    );
    retry?.click();
    await settle(el);
    expect(el.shadowRoot?.querySelector("[role='alert']")).toBeNull();
  });
});
