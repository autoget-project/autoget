// @vitest-environment jsdom
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";

import type { Resource, ResourcesResponse } from "../utils/api";

vi.mock("../utils/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../utils/api")>()),
  fetchIndexerResources:
    vi.fn<
      (
        indexer: string,
        category: string,
        keyword: string,
        page: number,
        pageSize?: number,
      ) => Promise<ResourcesResponse>
    >(),
}));
vi.mock("./download_button.js", () => ({}));

import * as api from "../utils/api";
import "./resource_list.ts";

type List = HTMLElement & {
  indexerId: string;
  category: string;
  keyword: string;
  page: number;
  updateComplete: Promise<boolean>;
};

const fetchRes = vi.mocked(api.fetchIndexerResources);

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

function response(resources: Resource[], totalPages = 1): ResourcesResponse {
  return {
    pagination: { page: 1, totalPages, pageSize: 100, total: resources.length },
    resources,
  };
}

async function settle(el: List): Promise<void> {
  for (let i = 0; i < 4; i++) {
    await Promise.resolve();
    await el.updateComplete;
  }
}

async function mount(props: Partial<Pick<List, "indexerId" | "page">> = {}): Promise<List> {
  const el = document.createElement("resource-list") as List;
  el.indexerId = props.indexerId ?? "idx";
  el.page = props.page ?? 1;
  document.body.appendChild(el);
  await settle(el);
  return el;
}

describe("resource-list", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
    localStorage.clear();
    fetchRes.mockReset();
    vi.stubGlobal(
      "matchMedia",
      vi.fn<(q: string) => MediaQueryList>().mockReturnValue({
        addEventListener: vi.fn<() => void>(),
        removeEventListener: vi.fn<() => void>(),
      } as unknown as MediaQueryList),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("renders a card per resource", async () => {
    fetchRes.mockResolvedValue(
      response([resource({ id: "1", title: "Alpha" }), resource({ id: "2", title: "Beta" })]),
    );
    const el = await mount();
    expect(fetchRes).toHaveBeenCalledWith("idx", "", "", 1);
    const titles = [...(el.shadowRoot?.querySelectorAll("h3") ?? [])].map((h) =>
      h.textContent?.trim(),
    );
    expect(titles).toEqual(["Alpha", "Beta"]);
    expect(el.shadowRoot?.querySelectorAll("download-button").length).toBe(2);
  });

  it("shows the empty state", async () => {
    fetchRes.mockResolvedValue(response([]));
    const el = await mount();
    expect(el.shadowRoot?.textContent).toContain("No resources found");
  });

  it("does not fetch without an indexer", async () => {
    const el = await mount({ indexerId: "" });
    expect(fetchRes).not.toHaveBeenCalled();
    expect(el.shadowRoot?.textContent).toContain("No resources found");
  });

  it("shows an error alert and retries", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    fetchRes.mockRejectedValueOnce(new Error("kaput"));
    const el = await mount();
    expect(el.shadowRoot?.querySelector('[role="alert"]')?.textContent).toContain("kaput");

    fetchRes.mockResolvedValueOnce(response([resource({ title: "Recovered" })]));
    el.shadowRoot?.querySelector<HTMLButtonElement>("button.btn-sm")?.click();
    await settle(el);
    expect(el.shadowRoot?.querySelector('[role="alert"]')).toBeNull();
    expect(el.shadowRoot?.querySelector("h3")?.textContent?.trim()).toBe("Recovered");
  });

  it("refetches when the page property changes", async () => {
    fetchRes.mockResolvedValue(response([resource()], 3));
    const el = await mount();
    el.page = 2;
    await settle(el);
    expect(fetchRes).toHaveBeenLastCalledWith("idx", "", "", 2);
    expect(fetchRes).toHaveBeenCalledTimes(2);
  });

  it("renders pagination and navigates via history", async () => {
    fetchRes.mockResolvedValue(response([resource()], 3));
    const el = await mount();
    const onPop = vi.fn<() => void>();
    window.addEventListener("popstate", onPop);
    const buttons = [...(el.shadowRoot?.querySelectorAll(".join button") ?? [])];
    const three = buttons.find((b) => b.textContent?.trim() === "3") as HTMLButtonElement;
    three.click();
    window.removeEventListener("popstate", onPop);
    expect(new URL(window.location.href).searchParams.get("page")).toBe("3");
    expect(onPop).toHaveBeenCalled();
  });

  it("does not render unsafe db links or details URLs as hrefs", async () => {
    fetchRes.mockResolvedValue(
      response([
        resource({
          detailsUrl: "javascript:alert(1)",
          dbs: [
            { db: "imdb", link: "javascript:alert(2)", rating: "7" },
            { db: "douban", link: "https://movie.douban.com/x", rating: "8" },
          ],
        }),
      ]),
    );
    const el = await mount();
    const hrefs = [...(el.shadowRoot?.querySelectorAll("a") ?? [])].map((a) =>
      a.getAttribute("href"),
    );
    expect(hrefs).toEqual(["https://movie.douban.com/x"]);
    const a = el.shadowRoot?.querySelector("a");
    expect(a?.getAttribute("rel")).toContain("noopener");
  });

  it("ignores a stale response when props change quickly", async () => {
    let resolveFirst!: (r: ResourcesResponse) => void;
    fetchRes.mockReturnValueOnce(new Promise((r) => (resolveFirst = r)));
    fetchRes.mockResolvedValueOnce(response([resource({ title: "Fresh" })]));
    const el = await mount();
    el.keyword = "new";
    await settle(el);
    resolveFirst(response([resource({ title: "Stale" })]));
    await settle(el);
    const titles = [...(el.shadowRoot?.querySelectorAll("h3") ?? [])].map((h) =>
      h.textContent?.trim(),
    );
    expect(titles).toEqual(["Fresh"]);
  });

  it("persists the zoom column count", async () => {
    fetchRes.mockResolvedValue(response([resource()]));
    const el = await mount();
    el.shadowRoot
      ?.querySelector<HTMLButtonElement>('button[aria-label="Zoom in (decrease columns)"]')
      ?.click();
    expect(localStorage.getItem("resource-list-column-count")).toBe("3");
  });
});
