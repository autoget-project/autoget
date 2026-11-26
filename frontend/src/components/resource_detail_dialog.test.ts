// @vitest-environment jsdom
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";

import type { Resource, ResourceDetail } from "../utils/api";

vi.mock("../utils/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../utils/api")>()),
  fetchResourceDetail: vi.fn<(indexer: string, id: string) => Promise<ResourceDetail>>(),
}));
vi.mock("./download_button.js", () => ({}));

import * as api from "../utils/api";
import "./resource_detail_dialog.ts";

type Dlg = HTMLElement & {
  open(indexerId: string, resource: Resource): void;
  updateComplete: Promise<boolean>;
};

const fetchDetail = vi.mocked(api.fetchResourceDetail);

function resource(over: Partial<Resource> = {}): Resource {
  return {
    id: "1",
    title: "Title One",
    title2: "",
    createdDate: 1700000000,
    category: "Movies",
    size: 1024,
    resolution: "",
    seeders: 5,
    leechers: 0,
    dbs: [],
    images: [],
    free: false,
    labels: [],
    ...over,
  };
}

async function settle(el: Dlg): Promise<void> {
  for (let i = 0; i < 4; i++) {
    await Promise.resolve();
    await el.updateComplete;
  }
}

async function mount(): Promise<Dlg> {
  const el = document.createElement("resource-detail-dialog") as Dlg;
  document.body.appendChild(el);
  await el.updateComplete;
  return el;
}

describe("resource-detail-dialog", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
    fetchDetail.mockReset();
    HTMLDialogElement.prototype.showModal = vi.fn<() => void>(function (this: HTMLDialogElement) {
      this.setAttribute("open", "");
    });
    HTMLDialogElement.prototype.close = vi.fn<() => void>(function (this: HTMLDialogElement) {
      this.removeAttribute("open");
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("shows list data at once, then files, mediainfo and description", async () => {
    fetchDetail.mockResolvedValue({
      ...resource(),
      mediainfo: "MI",
      description: "DESC",
      files: [
        { name: "a.mkv", size: 1024 },
        { name: "b.srt", size: 2048 },
      ],
    });
    const el = await mount();
    el.open("idx", resource({ title: "Alpha" }));
    await el.updateComplete;
    expect(HTMLDialogElement.prototype.showModal).toHaveBeenCalled();
    expect(el.shadowRoot?.querySelector("#detail-title")?.textContent).toContain("Alpha");
    await settle(el);
    expect(fetchDetail).toHaveBeenCalledWith("idx", "1");
    const rows = el.shadowRoot?.querySelectorAll("table tr") ?? [];
    expect(rows.length).toBe(2);
    expect(el.shadowRoot?.textContent).toContain("Files (2, 3 KB)");
    expect(el.shadowRoot?.textContent).toContain("MI");
    expect(el.shadowRoot?.textContent).toContain("DESC");
  });

  it("renders the description as text, not HTML", async () => {
    fetchDetail.mockResolvedValue({ ...resource(), description: "<script>x</script>" });
    const el = await mount();
    el.open("idx", resource());
    await settle(el);
    expect(el.shadowRoot?.querySelector("pre script")).toBeNull();
    expect(el.shadowRoot?.querySelector("pre")?.textContent).toBe("<script>x</script>");
  });

  it("shows an error and retries", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    fetchDetail.mockRejectedValueOnce(new Error("kaput"));
    const el = await mount();
    el.open("idx", resource());
    await settle(el);
    expect(el.shadowRoot?.querySelector('[role="alert"]')?.textContent).toContain("kaput");

    fetchDetail.mockResolvedValueOnce({ ...resource(), mediainfo: "OK-MI" });
    el.shadowRoot?.querySelector<HTMLButtonElement>('[role="alert"] button')?.click();
    await settle(el);
    expect(el.shadowRoot?.querySelector('[role="alert"]')).toBeNull();
    expect(el.shadowRoot?.textContent).toContain("OK-MI");
  });

  it("ignores a stale response", async () => {
    let resolveFirst!: (d: ResourceDetail) => void;
    fetchDetail.mockReturnValueOnce(new Promise((r) => (resolveFirst = r)));
    fetchDetail.mockResolvedValueOnce({ ...resource({ id: "2" }), description: "SECOND" });
    const el = await mount();
    el.open("idx", resource({ id: "1" }));
    el.open("idx", resource({ id: "2" }));
    await settle(el);
    resolveFirst({ ...resource({ id: "1" }), description: "FIRST" });
    await settle(el);
    expect(el.shadowRoot?.textContent).toContain("SECOND");
    expect(el.shadowRoot?.textContent).not.toContain("FIRST");
  });

  it("closes from the close button", async () => {
    fetchDetail.mockResolvedValue(resource());
    const el = await mount();
    el.open("idx", resource());
    await settle(el);
    el.shadowRoot?.querySelector<HTMLButtonElement>('button[aria-label="Close"]')?.click();
    expect(HTMLDialogElement.prototype.close).toHaveBeenCalled();
  });

  it("falls back to click-outside closing without closedby support", async () => {
    fetchDetail.mockResolvedValue(resource());
    const el = await mount();
    el.open("idx", resource());
    await settle(el);
    const dialog = el.shadowRoot?.querySelector("dialog") as HTMLDialogElement;
    dialog.getBoundingClientRect = () =>
      ({ left: 10, right: 100, top: 10, bottom: 100 }) as DOMRect;
    dialog.dispatchEvent(new MouseEvent("click", { clientX: 50, clientY: 50 }));
    expect(HTMLDialogElement.prototype.close).not.toHaveBeenCalled();
    dialog.dispatchEvent(new MouseEvent("click", { clientX: 5, clientY: 5 }));
    expect(HTMLDialogElement.prototype.close).toHaveBeenCalled();
  });
});
