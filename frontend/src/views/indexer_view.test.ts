// @vitest-environment jsdom
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";

import type { Category } from "../utils/api";

vi.mock("../utils/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../utils/api")>()),
  fetchIndexerCategories: vi.fn<(indexer: string) => Promise<Category[]>>(),
}));
vi.mock("../components/navbar.ts", () => ({}));
vi.mock("../components/resource_list.ts", () => ({}));

import * as api from "../utils/api";
import "./indexer_view.ts";

type View = HTMLElement & {
  indexerId: string;
  category: string;
  page: number;
  updateComplete: Promise<boolean>;
};

const fetchCategories = vi.mocked(api.fetchIndexerCategories);

async function settle(el: View): Promise<void> {
  for (let i = 0; i < 4; i++) {
    await Promise.resolve();
    await el.updateComplete;
  }
}

async function mount(): Promise<View> {
  const el = document.createElement("indexer-view") as View;
  el.indexerId = "mteam";
  el.category = "c2";
  el.page = 3;
  document.body.appendChild(el);
  await settle(el);
  return el;
}

describe("indexer-view", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
    localStorage.clear();
    fetchCategories.mockReset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders categories, passes props to resource-list and sets the title", async () => {
    fetchCategories.mockResolvedValue([
      { id: "c1", name: "Movies", subCategories: [{ id: "c2", name: "Action" }] },
    ] as Category[]);
    const el = await mount();
    expect(fetchCategories).toHaveBeenCalledWith("mteam");
    const links = [...(el.shadowRoot?.querySelectorAll("#left-panel-categories a") ?? [])].map(
      (a) => a.getAttribute("href"),
    );
    expect(links).toEqual(["/indexers/mteam/c1", "/indexers/mteam/c2"]);
    const list = el.shadowRoot?.querySelector("resource-list") as unknown as {
      indexerId: string;
      category: string;
      page: number;
    };
    expect(list.indexerId).toBe("mteam");
    expect(list.category).toBe("c2");
    expect(list.page).toBe(3);
    expect(document.title).toBe("AutoGet - mteam - Action - Page 3");
  });

  it("resizes the sidebar with arrow keys, clamps and persists", async () => {
    fetchCategories.mockResolvedValue([] as Category[]);
    const el = await mount();
    const sep = () => el.shadowRoot?.querySelector('[role="separator"]') as HTMLElement;
    expect(sep().getAttribute("aria-valuenow")).toBe("256");
    sep().dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true }));
    await el.updateComplete;
    expect(sep().getAttribute("aria-valuenow")).toBe("272");
    expect(localStorage.getItem("indexer-sidebar-width")).toBe("272");
    for (let i = 0; i < 30; i++) {
      sep().dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowLeft", bubbles: true }));
    }
    await el.updateComplete;
    expect(sep().getAttribute("aria-valuenow")).toBe("160");
    for (let i = 0; i < 60; i++) {
      sep().dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true }));
    }
    await el.updateComplete;
    expect(sep().getAttribute("aria-valuenow")).toBe("600");
  });

  it("makes the resize handle unfocusable when the sidebar is collapsed", async () => {
    fetchCategories.mockResolvedValue([] as Category[]);
    const el = await mount();
    const sep = () => el.shadowRoot?.querySelector('[role="separator"]') as HTMLElement;
    expect(sep().getAttribute("tabindex")).toBe("0");
    expect(sep().getAttribute("aria-hidden")).toBe("false");
    (el as unknown as { sidebarVisible: boolean }).sidebarVisible = false;
    await el.updateComplete;
    expect(sep().getAttribute("tabindex")).toBe("-1");
    expect(sep().getAttribute("aria-hidden")).toBe("true");
  });

  it("shows an error with Retry when categories fail to load", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    fetchCategories.mockRejectedValue(new Error("nope"));
    const el = await mount();
    const alert = el.shadowRoot?.querySelector('[role="alert"]');
    expect(alert?.textContent).toContain("Failed to load categories: nope");

    fetchCategories.mockResolvedValue([{ id: "c1", name: "Movies" }] as Category[]);
    alert?.querySelector<HTMLButtonElement>("button")?.click();
    await settle(el);
    expect(el.shadowRoot?.querySelector('[role="alert"]')).toBeNull();
    expect(el.shadowRoot?.querySelectorAll("#left-panel-categories li").length).toBe(1);
  });

  it("renders without waiting for categories and fetches once per indexer change", async () => {
    fetchCategories.mockReturnValue(new Promise(() => {}));
    const el = await mount();
    expect(el.shadowRoot?.querySelector("resource-list")).not.toBeNull();
    expect(fetchCategories).toHaveBeenCalledTimes(1);
    el.indexerId = "nyaa";
    await settle(el);
    expect(fetchCategories).toHaveBeenCalledTimes(2);
    expect(fetchCategories).toHaveBeenLastCalledWith("nyaa");
  });

  it("reacts to viewport breakpoint changes", async () => {
    fetchCategories.mockResolvedValue([] as Category[]);
    let listener: ((e: MediaQueryListEvent) => void) | undefined;
    vi.stubGlobal(
      "matchMedia",
      vi.fn<(q: string) => MediaQueryList>().mockReturnValue({
        matches: false,
        addEventListener: (_: string, l: (e: MediaQueryListEvent) => void) => (listener = l),
        removeEventListener: vi.fn<() => void>(),
      } as unknown as MediaQueryList),
    );
    const el = await mount();
    const root = el.shadowRoot as ShadowRoot;
    const panel = () => root.getElementById("left-panel-categories") as HTMLElement;
    expect(root.querySelector(".fixed.inset-0")).toBeNull();
    // Open the mobile drawer: backdrop appears.
    root.querySelector("div")?.dispatchEvent(new CustomEvent("sidebar-toggle"));
    await el.updateComplete;
    expect(root.querySelector(".fixed.inset-0")).not.toBeNull();
    expect(panel().getAttribute("style")).toContain("width: 280px");
    // Crossing into desktop drops the backdrop and uses the resizable width.
    listener?.({ matches: true } as MediaQueryListEvent);
    await el.updateComplete;
    expect(root.querySelector(".fixed.inset-0")).toBeNull();
    expect(panel().getAttribute("style")).toContain("width: 256px");
    vi.unstubAllGlobals();
  });

  it("encodes the indexer and category ids in category links", async () => {
    fetchCategories.mockResolvedValue([{ id: "a:b/c", name: "Adult" }] as Category[]);
    const el = await mount();
    el.indexerId = "m x";
    await settle(el);
    const href = el.shadowRoot?.querySelector("#left-panel-categories a")?.getAttribute("href");
    expect(href).toBe("/indexers/m%20x/a%3Ab%2Fc");
  });
});
