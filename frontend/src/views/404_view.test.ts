// @vitest-environment jsdom
import { describe, expect, it } from "vitest";

import "./404_view.ts";

describe("not-found-view", () => {
  it("renders the 404 message with a home link", async () => {
    const el = document.createElement("not-found-view") as HTMLElement & {
      updateComplete: Promise<boolean>;
    };
    document.body.appendChild(el);
    await el.updateComplete;
    expect(el.shadowRoot?.querySelector("h1")?.textContent).toBe("404");
    expect(el.shadowRoot?.querySelector("a")?.getAttribute("href")).toBe("/");
  });
});
