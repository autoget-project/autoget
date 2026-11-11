import { describe, expect, it, vi } from "vitest";
import { defaultCategoryId, resolveIndexers } from "./router_helpers.ts";
import type { Category } from "./api.ts";

describe("resolveIndexers", () => {
  it("returns immediately on first non-empty result", async () => {
    const fetcher = vi.fn<() => Promise<string[]>>().mockResolvedValue(["a"]);
    const sleep = vi.fn<(ms: number) => Promise<void>>().mockResolvedValue(undefined);
    expect(await resolveIndexers(fetcher, { sleep })).toEqual(["a"]);
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(sleep).not.toHaveBeenCalled();
  });

  it("retries on empty and then succeeds", async () => {
    const fetcher = vi
      .fn<() => Promise<any>>()
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce(["a"]);
    const sleep = vi.fn<(ms: number) => Promise<void>>().mockResolvedValue(undefined);
    expect(await resolveIndexers(fetcher, { sleep })).toEqual(["a"]);
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(sleep).toHaveBeenCalledTimes(1);
    expect(sleep).toHaveBeenCalledWith(500);
  });

  it("stops after maxAttempts with exponential backoff", async () => {
    const fetcher = vi.fn<() => Promise<string[]>>().mockResolvedValue([]);
    const sleep = vi.fn<(ms: number) => Promise<void>>().mockResolvedValue(undefined);
    expect(await resolveIndexers(fetcher, { sleep, maxAttempts: 3 })).toEqual([]);
    expect(fetcher).toHaveBeenCalledTimes(3);
    expect(sleep.mock.calls.map((c) => c[0])).toEqual([500, 1000]);
  });

  it("treats rejections as failed attempts", async () => {
    const fetcher = vi
      .fn<() => Promise<any>>()
      .mockRejectedValueOnce(new Error("x"))
      .mockResolvedValueOnce(["a"]);
    const sleep = vi.fn<(ms: number) => Promise<void>>().mockResolvedValue(undefined);
    expect(await resolveIndexers(fetcher, { sleep })).toEqual(["a"]);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
});

describe("defaultCategoryId", () => {
  it("returns undefined for empty list", () => {
    expect(defaultCategoryId([])).toBeUndefined();
  });
  it("returns first id", () => {
    expect(defaultCategoryId([{ id: "a" } as Category])).toBe("a");
  });
});
