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
  organizeDownload: vi.fn<(id: string, action: OrganizeAction, hint?: string) => Promise<void>>(),
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
const deleteDownload = vi.mocked(api.deleteDownload);

function item(over: Partial<DownloadItem> = {}): DownloadItem {
  return {
    ID: "d1",
    CreatedAt: "2024-01-01T00:00:00Z",
    UpdatedAt: "2024-01-01T00:00:00Z",
    Downloader: "tr",
    DownloadProgress: 0.5,
    State: 0,
    ResIndexer: "mteam",
    ResTitle: "Some Torrent",
    ResTitle2: "",
    Category: "Movies",
    FileList: [],
    Metadata: {
      actors: [],
      category: "",
      description: "",
      dmm_id: "",
      labels: [],
      organizer_category: [],
      title: "",
    },
    MoveState: 0,
    OrganizeState: 0,
    OrganizePlans: null,
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
    fetchItems.mockResolvedValue(status([item({ ResTitle: "Some Torrent" })]));
    const el = await mount();
    expect(el.shadowRoot?.textContent).toContain("Some Torrent");
    expect(el.shadowRoot?.textContent).not.toContain("No active downloads");
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

  it("does not delete when the confirmation is declined", async () => {
    fetchItems.mockResolvedValue(status([item()]));
    vi.stubGlobal("confirm", vi.fn<(m?: string) => boolean>().mockReturnValue(false));
    const el = await mount();
    el.shadowRoot?.querySelector<HTMLButtonElement>("button.btn-error")?.click();
    await settle(el);
    expect(deleteDownload).not.toHaveBeenCalled();
    vi.unstubAllGlobals();
  });
});
