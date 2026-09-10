// @vitest-environment jsdom
import { describe, it, expect, beforeEach, vi } from "vitest";
import { THEMES, getStoredTheme, applyTheme, initTheme } from "./theme";

describe("theme utility", () => {
  let mockStorage: Record<string, string> = {};

  beforeEach(() => {
    mockStorage = {};
    const storageMock = {
      getItem: (key: string) => mockStorage[key] ?? null,
      setItem: (key: string, value: string) => {
        mockStorage[key] = value;
      },
      removeItem: (key: string) => {
        delete mockStorage[key];
      },
      clear: () => {
        mockStorage = {};
      },
    };
    Object.defineProperty(window, "localStorage", {
      value: storageMock,
      configurable: true,
      writable: true,
    });
    Object.defineProperty(globalThis, "localStorage", {
      value: storageMock,
      configurable: true,
      writable: true,
    });
    document.documentElement.removeAttribute("data-theme");
    document.documentElement.className = "";
  });

  it("should contain default light, dark and catppuccin themes", () => {
    const themeIds = THEMES.map((t) => t.id);
    expect(themeIds).toContain("light");
    expect(themeIds).toContain("dark");
    expect(themeIds).toContain("latte");
    expect(themeIds).toContain("frappe");
    expect(themeIds).toContain("macchiato");
    expect(themeIds).toContain("mocha");
  });

  it("should default to light when no preference is saved", () => {
    expect(getStoredTheme()).toBe("light");
  });

  it("should return stored theme if valid", () => {
    localStorage.setItem("autoget-theme", "mocha");
    expect(getStoredTheme()).toBe("mocha");
  });

  it("should apply theme to documentElement and add dark class for dark themes", () => {
    applyTheme("mocha");
    expect(document.documentElement.getAttribute("data-theme")).toBe("mocha");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(localStorage.getItem("autoget-theme")).toBe("mocha");

    applyTheme("latte");
    expect(document.documentElement.getAttribute("data-theme")).toBe("latte");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    expect(localStorage.getItem("autoget-theme")).toBe("latte");
  });

  it("should dispatch autoget-theme-changed event when theme changes", () => {
    const handler = vi.fn();
    window.addEventListener("autoget-theme-changed", handler);

    applyTheme("frappe");
    expect(handler).toHaveBeenCalledTimes(1);

    window.removeEventListener("autoget-theme-changed", handler);
  });

  it("initTheme should read stored theme and apply it", () => {
    localStorage.setItem("autoget-theme", "macchiato");
    const current = initTheme();
    expect(current).toBe("macchiato");
    expect(document.documentElement.getAttribute("data-theme")).toBe("macchiato");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });
});
