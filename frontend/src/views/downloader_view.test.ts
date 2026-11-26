// @vitest-environment jsdom
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";

import type {
  DownloadItem,
  DownloadState,
  DownloaderStatusResponse,
  OrganizeAction,
} from "../utils/api";

vi.mock("../utils/api.ts", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../utils/api.ts")>()),
  fetchDownloaderItems:
    vi.fn<(name: string, state: DownloadState) => Promise<DownloaderStatusResponse>>(),
  organizeDownload:
    vi.fn<(id: string, action: OrganizeAction, hint?: string) => Promise<api.OrganizeResult>>(),
  deleteDownload: vi.fn<(id: string) => Promise<void>>(),
}));
vi.mock("../components/navbar.ts", () => ({}));

import * as api from "../utils/api.ts";
import "./downloader_view.ts";

type View = HTMLElement & {
  downloaderId: string;
  activeTab: string;
  updateComplete: Promise<boolean>;
};

const fetchItems = vi.mocked(api.fetchDownloaderItems);
const organize = vi.mocked(api.organizeDownload);
const deleteDownload = vi.mocked(api.deleteDownload);

function item(over: Partial<DownloadItem> = {}): DownloadItem {
  return {
    id: "d1",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    downloader: "tr",
    download_progress: 0.5,
    state: 0,
    res_indexer: "mteam",
    res_title: "Some Torrent",
    res_title2: "",
    category: "Movies",
    file_list: [],
    metadata: {
      actors: [],
      category: "",
      description: "",
      dmm_id: "",
      labels: [],
      organizer_category: [],
      title: "",
    },
    move_state: 0,
    organize_state: 0,
    organize_plans: null,
    ...over,
  };
}

function status(resources: DownloadItem[]): DownloaderStatusResponse {
  return {
    state: { count_of_downloading: resources.length, count_of_planned: 0, count_of_failed: 0 },
    resources,
  };
}

async function settle(el: View): Promise<void> {
  for (let i = 0; i < 6; i++) {
    await vi.advanceTimersByTimeAsync(0);
    await el.updateComplete;
  }
}

async function mount(tab = "downloading"): Promise<View> {
  const el = document.createElement("downloader-view") as View;
  el.downloaderId = "tr";
  el.activeTab = tab;
  document.body.appendChild(el);
  await settle(el);
  return el;
}

describe("downloader-view", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    document.body.innerHTML = "";
    fetchItems.mockReset();
    deleteDownload.mockReset();
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  afterEach(() => {
    document.body.innerHTML = "";
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("renders the empty message when there are no items", async () => {
    fetchItems.mockResolvedValue(status([]));
    const el = await mount();
    expect(fetchItems).toHaveBeenCalledWith("tr", "downloading");
    expect(el.shadowRoot?.textContent).toContain("No active downloads at the moment.");
  });

  it("renders downloads when the fetch succeeds", async () => {
    fetchItems.mockResolvedValue(status([item({ res_title: "Some Torrent" })]));
    const el = await mount();
    expect(el.shadowRoot?.textContent).toContain("Some Torrent");
    expect(el.shadowRoot?.textContent).not.toContain("No active downloads");
  });

  it("fetches once on mount without touching history", async () => {
    const push = vi.spyOn(history, "pushState");
    fetchItems.mockResolvedValue(status([]));
    await mount();
    expect(fetchItems).toHaveBeenCalledTimes(1);
    expect(push).not.toHaveBeenCalled();
  });

  it("pushes history once when a tab is clicked", async () => {
    const push = vi.spyOn(history, "pushState");
    fetchItems.mockResolvedValue(status([]));
    const el = await mount();
    const tab = [
      ...(el.shadowRoot?.querySelectorAll<HTMLButtonElement>('[role="tab"]') ?? []),
    ].find((b) => b.textContent?.includes("Seeding"));
    tab?.click();
    await settle(el);
    expect(push).toHaveBeenCalledTimes(1);
    expect(push).toHaveBeenCalledWith(null, "", "/downloaders/tr/seeding");
    expect(fetchItems).toHaveBeenLastCalledWith("tr", "seeding");
  });

  it("does not push history when activeTab is set programmatically", async () => {
    const push = vi.spyOn(history, "pushState");
    fetchItems.mockResolvedValue(status([]));
    const el = await mount();
    el.activeTab = "failed";
    await settle(el);
    expect(push).not.toHaveBeenCalled();
    expect(fetchItems).toHaveBeenLastCalledWith("tr", "failed");
  });

  it("ignores a stale response from a previous tab", async () => {
    let resolveFirst!: (v: DownloaderStatusResponse) => void;
    fetchItems.mockImplementationOnce(
      () => new Promise<DownloaderStatusResponse>((r) => (resolveFirst = r)),
    );
    fetchItems.mockResolvedValueOnce(status([item({ res_title: "Second Tab Item" })]));
    const el = await mount();
    el.activeTab = "seeding";
    await settle(el);
    expect(el.shadowRoot?.textContent).toContain("Second Tab Item");
    resolveFirst(status([item({ id: "d2", res_title: "First Tab Item" })]));
    await settle(el);
    expect(el.shadowRoot?.textContent).toContain("Second Tab Item");
    expect(el.shadowRoot?.textContent).not.toContain("First Tab Item");
  });

  it("shows the spinner again when downloaderId changes", async () => {
    fetchItems.mockResolvedValueOnce(status([item({ res_title: "Old Item" })]));
    const el = await mount();
    expect(el.shadowRoot?.textContent).toContain("Old Item");
    let resolveNext!: (v: DownloaderStatusResponse) => void;
    fetchItems.mockImplementationOnce(
      () => new Promise<DownloaderStatusResponse>((r) => (resolveNext = r)),
    );
    el.downloaderId = "other";
    await settle(el);
    expect(el.shadowRoot?.querySelector(".loading-spinner, .loading")).not.toBeNull();
    expect(el.shadowRoot?.textContent).not.toContain("Old Item");
    resolveNext(status([item({ res_title: "New Item" })]));
    await settle(el);
    expect(el.shadowRoot?.textContent).toContain("New Item");
  });

  it("shows an error with Retry on load failure and recovers", async () => {
    fetchItems.mockRejectedValue(new Error("backend down"));
    const el = await mount();
    const alert = el.shadowRoot?.querySelector('[role="alert"]');
    expect(alert?.textContent).toContain("Failed to load downloads: backend down");

    fetchItems.mockResolvedValue(status([item()]));
    alert?.querySelector<HTMLButtonElement>("button")?.click();
    await settle(el);
    expect(el.shadowRoot?.querySelector('[role="alert"]')).toBeNull();
    expect(el.shadowRoot?.textContent).toContain("Some Torrent");
  });

  it("polls for updates on the configured interval", async () => {
    fetchItems.mockResolvedValue(status([]));
    const el = await mount();
    const before = fetchItems.mock.calls.length;
    await vi.advanceTimersByTimeAsync(20000);
    expect(fetchItems.mock.calls.length).toBe(before + 1);
    el.remove();
    await vi.advanceTimersByTimeAsync(60000);
    expect(fetchItems.mock.calls.length).toBe(before + 1);
  });

  it("surfaces a delete failure as a dismissible alert", async () => {
    fetchItems.mockResolvedValue(status([item()]));
    deleteDownload.mockRejectedValue(new Error("cannot delete"));
    vi.stubGlobal("confirm", vi.fn<(m?: string) => boolean>().mockReturnValue(true));
    const el = await mount();
    el.shadowRoot?.querySelector<HTMLButtonElement>("button.btn-error")?.click();
    await settle(el);
    expect(deleteDownload).toHaveBeenCalledWith("d1");
    const alert = el.shadowRoot?.querySelector('[role="alert"]');
    expect(alert?.textContent).toContain("Failed to delete download: cannot delete");

    alert?.querySelector<HTMLButtonElement>('button[aria-label="Dismiss"]')?.click();
    await settle(el);
    expect(el.shadowRoot?.querySelector('[role="alert"]')).toBeNull();
    vi.unstubAllGlobals();
  });

  it("shows failed moves of a partially executed plan", async () => {
    fetchItems.mockResolvedValue(status([item()]));
    organize.mockResolvedValue({
      status: "partial",
      failed: { failed_move: [{ file: "a.mkv", action: "move", reason: "target exists" }] },
    });
    const el = await mount("planned");
    el.shadowRoot?.querySelector<HTMLButtonElement>("button.btn-success")?.click();
    await settle(el);
    const alert = el.shadowRoot?.querySelector('[role="alert"]');
    expect(alert?.textContent).toContain("1 move(s) failed: a.mkv: target exists");
  });

  it("shows the plan error even when the plan is empty", async () => {
    fetchItems.mockResolvedValue(status([item({ organize_plans: { plan: [], error: "x" } })]));
    const el = await mount();
    expect(el.shadowRoot?.textContent).toContain("Plan Error: x");
  });

  it("does not delete when the confirmation is declined", async () => {
    fetchItems.mockResolvedValue(status([item()]));
    vi.stubGlobal("confirm", vi.fn<(m?: string) => boolean>().mockReturnValue(false));
    const el = await mount();
    el.shadowRoot?.querySelector<HTMLButtonElement>("button.btn-error")?.click();
    await settle(el);
    expect(deleteDownload).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });

  it("ignores a second delete while the first is in flight", async () => {
    fetchItems.mockResolvedValue(status([item()]));
    let resolve!: () => void;
    deleteDownload.mockReturnValue(new Promise<void>((r) => (resolve = r)));
    vi.stubGlobal("confirm", vi.fn<(m?: string) => boolean>().mockReturnValue(true));
    const el = await mount();
    const btn = () => el.shadowRoot?.querySelector<HTMLButtonElement>("button.btn-error");
    btn()?.click();
    btn()?.click();
    await settle(el);
    expect(deleteDownload).toHaveBeenCalledTimes(1);
    expect(btn()?.disabled).toBe(true);
    resolve();
    await settle(el);
    expect(btn()?.disabled).toBe(false);
    vi.unstubAllGlobals();
  });

  it("exposes tabs with aria-selected, roving tabindex and a labelled tabpanel", async () => {
    fetchItems.mockResolvedValue(status([]));
    const el = await mount("seeding");
    const tabs = [...(el.shadowRoot?.querySelectorAll<HTMLButtonElement>('[role="tab"]') ?? [])];
    const selected = tabs.filter((t) => t.getAttribute("aria-selected") === "true");
    expect(selected.length).toBe(1);
    expect(selected[0].id).toBe("tab-seeding");
    expect(selected[0].tabIndex).toBe(0);
    expect(tabs.filter((t) => t !== selected[0]).every((t) => t.tabIndex === -1)).toBe(true);
    const panel = el.shadowRoot?.querySelector('[role="tabpanel"]');
    expect(panel?.getAttribute("aria-labelledby")).toBe("tab-seeding");
    expect(selected[0].getAttribute("aria-controls")).toBe(panel?.id);

    selected[0].dispatchEvent(new KeyboardEvent("keydown", { key: "Home", bubbles: true }));
    await settle(el);
    expect(el.activeTab).toBe(tabs[0].id.replace("tab-", ""));
  });
});
