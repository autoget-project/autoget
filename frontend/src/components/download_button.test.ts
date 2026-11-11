// @vitest-environment jsdom
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";

vi.mock("../utils/api.js", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../utils/api.js")>()),
  downloadResource: vi.fn<(indexerId: string, resourceId: string) => Promise<void>>(),
}));

import * as api from "../utils/api.js";
import "./download_button.ts";

type Button = HTMLElement & {
  indexerId: string;
  resourceId: string;
  updateComplete: Promise<boolean>;
};

const downloadResource = vi.mocked(api.downloadResource);

async function mount(): Promise<Button> {
  const el = document.createElement("download-button") as Button;
  el.indexerId = "idx";
  el.resourceId = "res 1";
  document.body.appendChild(el);
  await el.updateComplete;
  return el;
}

function button(el: Button): HTMLButtonElement {
  const b = el.shadowRoot?.querySelector("button");
  if (!b) throw new Error("no button rendered");
  return b;
}

// Lets the awaited mock promise and the resulting re-render settle.
async function settle(el: Button): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
  await el.updateComplete;
}

describe("download-button", () => {
  beforeEach(() => {
    document.body.innerHTML = "";
    downloadResource.mockReset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders a Download button initially", async () => {
    const el = await mount();
    expect(button(el).textContent?.trim()).toBe("Download");
    expect(button(el).disabled).toBe(false);
  });

  it("calls downloadResource with the ids and shows Added", async () => {
    downloadResource.mockResolvedValue();
    const el = await mount();
    button(el).click();
    await settle(el);
    expect(downloadResource).toHaveBeenCalledExactlyOnceWith("idx", "res 1");
    expect(button(el).textContent?.trim()).toBe("Added");
    expect(button(el).disabled).toBe(true);
  });

  it("shows a disabled spinner while loading and ignores a second click", async () => {
    let resolve!: () => void;
    downloadResource.mockReturnValue(new Promise<void>((r) => (resolve = r)));
    const el = await mount();
    button(el).click();
    await el.updateComplete;
    expect(button(el).disabled).toBe(true);
    expect(el.shadowRoot?.querySelector(".loading")).not.toBeNull();
    button(el).click();
    expect(downloadResource).toHaveBeenCalledTimes(1);
    resolve();
    await settle(el);
    expect(button(el).textContent?.trim()).toBe("Added");
  });

  it("shows Already added on a 409 without a retry path", async () => {
    downloadResource.mockRejectedValue(new api.ApiError("conflict", 409));
    const el = await mount();
    button(el).click();
    await settle(el);
    expect(button(el).textContent?.trim()).toBe("Already added");
    expect(button(el).disabled).toBe(true);
  });

  it("shows Failed with the error text and allows retry", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    downloadResource.mockRejectedValueOnce(new Error("network down"));
    const el = await mount();
    button(el).click();
    await settle(el);
    expect(button(el).textContent?.trim()).toBe("Failed");
    expect(button(el).title).toBe("network down");
    expect(button(el).disabled).toBe(false);

    downloadResource.mockResolvedValueOnce();
    button(el).click();
    await settle(el);
    expect(downloadResource).toHaveBeenCalledTimes(2);
    expect(button(el).textContent?.trim()).toBe("Added");
  });
});
