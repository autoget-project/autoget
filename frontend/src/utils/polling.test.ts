// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createVisibilityPoller } from "./polling";

function setVisibility(state: "visible" | "hidden"): void {
  Object.defineProperty(document, "visibilityState", { value: state, configurable: true });
  document.dispatchEvent(new Event("visibilitychange"));
}

beforeEach(() => {
  vi.useFakeTimers();
  Object.defineProperty(document, "visibilityState", { value: "visible", configurable: true });
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("createVisibilityPoller", () => {
  it("ticks while visible, pauses while hidden, refreshes on return", () => {
    const fn = vi.fn<() => void>();
    const poller = createVisibilityPoller(fn, 1000);
    poller.start();
    vi.advanceTimersByTime(2500);
    expect(fn).toHaveBeenCalledTimes(2);

    setVisibility("hidden");
    vi.advanceTimersByTime(10_000);
    expect(fn).toHaveBeenCalledTimes(2);

    setVisibility("visible");
    expect(fn).toHaveBeenCalledTimes(3);
    vi.advanceTimersByTime(1000);
    expect(fn).toHaveBeenCalledTimes(4);
    poller.stop();
  });

  it("registers one listener, removes it on stop, and honours setInterval", () => {
    const add = vi.spyOn(document, "addEventListener");
    const remove = vi.spyOn(document, "removeEventListener");
    const fn = vi.fn<() => void>();
    const poller = createVisibilityPoller(fn, 1000);
    poller.start();
    poller.start();
    expect(add.mock.calls.filter((c) => c[0] === "visibilitychange")).toHaveLength(1);

    poller.setInterval(500);
    vi.advanceTimersByTime(1000);
    expect(fn).toHaveBeenCalledTimes(2);

    poller.stop();
    expect(remove.mock.calls.filter((c) => c[0] === "visibilitychange")).toHaveLength(1);
    vi.advanceTimersByTime(5000);
    expect(fn).toHaveBeenCalledTimes(2);
    setVisibility("hidden");
    setVisibility("visible");
    expect(fn).toHaveBeenCalledTimes(2);
  });

  it("does not tick when started while hidden", () => {
    setVisibility("hidden");
    const fn = vi.fn<() => void>();
    const poller = createVisibilityPoller(fn, 1000);
    poller.start();
    vi.advanceTimersByTime(5000);
    expect(fn).not.toHaveBeenCalled();
    poller.stop();
  });
});
