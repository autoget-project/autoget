// @vitest-environment jsdom
import { html, render } from "lit";
import { describe, expect, it, vi } from "vitest";

import { renderError } from "./error_alert";

function mountAlert(message: string, onRetry?: () => void, onDismiss?: () => void): HTMLElement {
  const host = document.createElement("div");
  render(html`${renderError(message, onRetry, onDismiss)}`, host);
  return host;
}

describe("renderError", () => {
  it("renders the message in an alert", () => {
    const host = mountAlert("boom");
    const alert = host.querySelector('[role="alert"]');
    expect(alert).not.toBeNull();
    expect(alert?.textContent).toContain("boom");
  });

  it("renders no buttons without callbacks", () => {
    const host = mountAlert("boom");
    expect(host.querySelector("button")).toBeNull();
  });

  it("escapes markup in the message", () => {
    const host = mountAlert("<img src=x>");
    expect(host.querySelector("img")).toBeNull();
    expect(host.textContent).toContain("<img src=x>");
  });

  it("invokes onRetry from the Retry button", () => {
    const onRetry = vi.fn<() => void>();
    const host = mountAlert("boom", onRetry);
    const btn = host.querySelector("button");
    expect(btn?.textContent?.trim()).toBe("Retry");
    btn?.click();
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("invokes onDismiss from the Dismiss button", () => {
    const onDismiss = vi.fn<() => void>();
    const host = mountAlert("boom", undefined, onDismiss);
    const btn = host.querySelector<HTMLButtonElement>('button[aria-label="Dismiss"]');
    expect(btn).not.toBeNull();
    btn?.click();
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });
});
