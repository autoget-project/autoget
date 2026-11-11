// @vitest-environment jsdom
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";

vi.mock("../utils/api", () => ({
  fetchIndexers: vi.fn<() => Promise<string[]>>(),
  fetchDownloaders: vi.fn<() => Promise<unknown[]>>(),
  isAuthenticated: vi.fn<() => boolean>(),
  logout: vi.fn<() => void>(),
}));
vi.mock("./theme_controller.ts", () => ({}));

import * as api from "../utils/api";
import "./navbar.ts";

type Navbar = HTMLElement & { activePage: string; updateComplete: Promise<boolean> };

const fetchIndexers = vi.mocked(api.fetchIndexers);
const fetchDownloaders = vi.mocked(api.fetchDownloaders);
const isAuthenticated = vi.mocked(api.isAuthenticated);
const logout = vi.mocked(api.logout);

const downloader = (name: string, failed = 0, planned = 0, downloading = 0) => ({
  name,
  count_of_failed: failed,
  count_of_planned: planned,
  count_of_downloading: downloading,
});

async function mount(activePage = ""): Promise<Navbar> {
  const el = document.createElement("app-navbar") as Navbar;
  el.activePage = activePage;
  document.body.appendChild(el);
  // connectedCallback awaits both fetches before the final requestUpdate.
  await vi.advanceTimersByTimeAsync(0);
  await el.updateComplete;
  return el;
}

describe("app-navbar", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    document.body.innerHTML = "";
    fetchIndexers.mockReset().mockResolvedValue(["mteam", "hdsky"]);
    fetchDownloaders.mockReset().mockResolvedValue([downloader("tr")]);
    isAuthenticated.mockReset().mockReturnValue(true);
    logout.mockReset();
  });

  afterEach(() => {
    document.body.innerHTML = "";
    vi.useRealTimers();
  });

  it("renders indexer and downloader links", async () => {
    const el = await mount();
    const hrefs = [...(el.shadowRoot?.querySelectorAll("a") ?? [])].map((a) =>
      a.getAttribute("href"),
    );
    expect(hrefs).toContain("/indexers/mteam");
    expect(hrefs).toContain("/indexers/hdsky");
    expect(hrefs).toContain("/downloaders/tr");
    expect(hrefs).toContain("/search");
  });

  it("highlights the active indexer tab", async () => {
    const el = await mount("hdsky");
    const active = el.shadowRoot?.querySelectorAll('[role="tab"].tab-active');
    expect(active?.length).toBe(1);
    expect(active?.[0].textContent?.trim()).toBe("hdsky");
  });

  it("highlights the active downloader link", async () => {
    const el = await mount("tr");
    const link = el.shadowRoot?.querySelector('a[href="/downloaders/tr"].btn-active');
    expect(link).not.toBeNull();
  });

  it("colors the downloader border by failed > planned > downloading", async () => {
    fetchDownloaders.mockResolvedValue([
      downloader("a", 1, 1, 1),
      downloader("b", 0, 1, 1),
      downloader("c", 0, 0, 1),
      downloader("d"),
    ]);
    const el = await mount();
    const cls = (name: string) =>
      el.shadowRoot?.querySelector(`.hidden.md\\:flex a[href="/downloaders/${name}"]`)?.className;
    expect(cls("a")).toContain("border-error");
    expect(cls("b")).toContain("border-info");
    expect(cls("c")).toContain("border-success");
    expect(cls("d")).not.toMatch(/border-(error|info|success)/);
  });

  it("shows the logout button when authenticated and calls logout", async () => {
    const el = await mount();
    const btn = el.shadowRoot?.querySelector<HTMLButtonElement>('button[aria-label="Log out"]');
    expect(btn).not.toBeNull();
    btn?.click();
    expect(logout).toHaveBeenCalledTimes(1);
  });

  it("hides the logout button when unauthenticated", async () => {
    isAuthenticated.mockReturnValue(false);
    const el = await mount();
    expect(el.shadowRoot?.querySelector('button[aria-label="Log out"]')).toBeNull();
  });

  it("still renders when fetching indexers fails", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    fetchIndexers.mockRejectedValue(new Error("down"));
    const el = await mount();
    expect(el.shadowRoot?.querySelector('a[href="/search"]')).not.toBeNull();
    expect(el.shadowRoot?.querySelector('a[href="/indexers/mteam"]')).toBeNull();
  });

  it("polls downloaders until disconnected", async () => {
    const el = await mount();
    expect(fetchDownloaders).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(20000);
    expect(fetchDownloaders).toHaveBeenCalledTimes(2);
    el.remove();
    await vi.advanceTimersByTimeAsync(60000);
    expect(fetchDownloaders).toHaveBeenCalledTimes(2);
  });

  it("dispatches sidebar-toggle from the toggle button on indexer pages", async () => {
    const el = await mount("mteam");
    const onToggle = vi.fn<() => void>();
    el.addEventListener("sidebar-toggle", onToggle);
    el.shadowRoot
      ?.querySelector<HTMLButtonElement>('button[aria-label="Toggle categories"]')
      ?.click();
    expect(onToggle).toHaveBeenCalledTimes(1);
  });
});
