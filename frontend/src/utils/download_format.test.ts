import { describe, it, expect } from "vitest";
import type { DownloadItem } from "./api.ts";
import {
  downloadItemsEqual,
  downloaderStatesEqual,
  getMoveStateLabel,
  getOrganizeStateLabel,
  getTabBadgeColor,
  getTabCount,
} from "./download_format.ts";

function item(overrides: Partial<DownloadItem> = {}): DownloadItem {
  return {
    id: "a",
    updated_at: "t1",
    download_progress: 100,
    state: 0,
    move_state: 0,
    organize_state: 0,
    organize_plans: null,
    ...overrides,
  } as DownloadItem;
}

describe("downloadItemsEqual", () => {
  it("treats identical lists as equal regardless of order", () => {
    expect(
      downloadItemsEqual(
        [item({ id: "a" }), item({ id: "b" })],
        [item({ id: "b" }), item({ id: "a" })],
      ),
    ).toBe(true);
  });

  it("detects length and id differences", () => {
    expect(downloadItemsEqual([item()], [])).toBe(false);
    expect(downloadItemsEqual([item({ id: "a" })], [item({ id: "b" })])).toBe(false);
  });

  it("detects changes in display-relevant fields", () => {
    expect(downloadItemsEqual([item()], [item({ download_progress: 200 })])).toBe(false);
    expect(downloadItemsEqual([item()], [item({ organize_state: 2 })])).toBe(false);
    expect(downloadItemsEqual([item()], [item({ updated_at: "t2" })])).toBe(false);
    expect(
      downloadItemsEqual([item()], [item({ organize_plans: { plan: [], error: "x" } as never })]),
    ).toBe(false);
  });
});

describe("downloaderStatesEqual", () => {
  const s = { count_of_downloading: 1, count_of_planned: 2, count_of_failed: 3 };
  it("handles nulls and values", () => {
    expect(downloaderStatesEqual(null, null)).toBe(true);
    expect(downloaderStatesEqual(s, null)).toBe(false);
    expect(downloaderStatesEqual(s, { ...s })).toBe(true);
    expect(downloaderStatesEqual(s, { ...s, count_of_failed: 0 })).toBe(false);
  });
});

describe("tab helpers", () => {
  const s = { count_of_downloading: 1, count_of_planned: 2, count_of_failed: 3 };
  it("getTabCount", () => {
    expect(getTabCount(null, "downloading")).toBe(0);
    expect(getTabCount(s, "downloading")).toBe(1);
    expect(getTabCount(s, "planned")).toBe(2);
    expect(getTabCount(s, "failed")).toBe(3);
    expect(getTabCount(s, "seeding")).toBe(0);
  });
  it("getTabBadgeColor", () => {
    expect(getTabBadgeColor("downloading")).toBe("badge-success");
    expect(getTabBadgeColor("planned")).toBe("badge-info");
    expect(getTabBadgeColor("failed")).toBe("badge-error");
    expect(getTabBadgeColor("stopped")).toBe("badge-neutral");
  });
});

describe("state labels", () => {
  it("getMoveStateLabel", () => {
    expect(getMoveStateLabel(0)).toEqual({ label: "Unmoved", color: "warning" });
    expect(getMoveStateLabel(1)).toEqual({ label: "Moved", color: "success" });
    expect(getMoveStateLabel(9)).toEqual({ label: "Unknown", color: "neutral" });
  });
  it("getOrganizeStateLabel", () => {
    expect(getOrganizeStateLabel(0).label).toBe("Unplanned");
    expect(getOrganizeStateLabel(1)).toEqual({ label: "Planned", color: "info" });
    expect(getOrganizeStateLabel(2)).toEqual({ label: "Organized", color: "success" });
    expect(getOrganizeStateLabel(3)).toEqual({ label: "Create Failed", color: "error" });
    expect(getOrganizeStateLabel(4)).toEqual({ label: "Execute Failed", color: "error" });
    expect(getOrganizeStateLabel(-1)).toEqual({ label: "Unknown", color: "neutral" });
  });
});
