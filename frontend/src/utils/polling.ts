export interface VisibilityPoller {
  start(): void;
  stop(): void;
  setInterval(ms: number): void;
}

// createVisibilityPoller calls fn every intervalMs while the page is visible.
// The timer is stopped while the tab is hidden (no wakeups, no background
// requests that could hit auth redirects) and fn runs immediately once the tab
// becomes visible again. It owns exactly one visibilitychange listener.
export function createVisibilityPoller(fn: () => void, intervalMs: number): VisibilityPoller {
  let interval = intervalMs;
  let timer: ReturnType<typeof setInterval> | null = null;
  let running = false;

  const isVisible = (): boolean => document.visibilityState !== "hidden";

  const clearTimer = (): void => {
    if (timer !== null) {
      clearInterval(timer);
      timer = null;
    }
  };

  const schedule = (): void => {
    clearTimer();
    if (running && isVisible()) {
      timer = setInterval(fn, interval);
    }
  };

  const onVisibility = (): void => {
    if (!running) {
      return;
    }
    if (isVisible()) {
      fn();
    }
    schedule();
  };

  return {
    start() {
      if (!running) {
        running = true;
        document.addEventListener("visibilitychange", onVisibility);
      }
      schedule();
    },
    stop() {
      running = false;
      document.removeEventListener("visibilitychange", onVisibility);
      clearTimer();
    },
    setInterval(ms: number) {
      interval = ms;
      schedule();
    },
  };
}
