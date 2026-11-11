// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactiveController, ReactiveControllerHost } from "lit";
import { Router, type Params, type RouteConfig } from "./router.ts";

class Host implements ReactiveControllerHost {
  controllers: ReactiveController[] = [];
  updates = 0;
  updateComplete = Promise.resolve(true);
  addController(c: ReactiveController) {
    this.controllers.push(c);
  }
  removeController() {}
  requestUpdate() {
    this.updates++;
  }
  connect() {
    this.controllers.forEach((c) => c.hostConnected?.());
  }
  disconnect() {
    this.controllers.forEach((c) => c.hostDisconnected?.());
  }
}

const tick = () => new Promise((r) => setTimeout(r, 0));

function routes(extra: Partial<Record<string, RouteConfig["enter"]>> = {}): RouteConfig[] {
  return [
    { path: "/", render: () => "home", enter: extra["/"] },
    { path: "/a/:id", render: (p: Params) => `a:${p.id}`, enter: extra["/a/:id"] },
    { path: "/a/:id/:tab", render: (p: Params) => `a:${p.id}:${p.tab}` },
    { path: "/a/special", render: () => "special" },
    { path: "*", render: () => "404" },
  ];
}

function setup(r: RouteConfig[] = routes()) {
  const host = new Host();
  const router = new Router(host, r);
  return { host, router };
}

function clickOn(el: Element, init: MouseEventInit = {}) {
  const ev = new MouseEvent("click", {
    bubbles: true,
    cancelable: true,
    composed: true,
    button: 0,
    ...init,
  });
  el.dispatchEvent(ev);
  return ev;
}

function anchor(href: string, attrs: Record<string, string> = {}) {
  const a = document.createElement("a");
  a.setAttribute("href", href);
  for (const [k, v] of Object.entries(attrs)) a.setAttribute(k, v);
  document.body.appendChild(a);
  return a;
}

beforeEach(() => {
  document.body.innerHTML = "";
  window.history.replaceState({}, "", "/");
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("matching", () => {
  it("renders the matching route", async () => {
    const { router } = setup();
    expect(router.outlet()).toBeUndefined();
    await router.goto("/");
    expect(router.outlet()).toBe("home");
  });

  it("first match wins over later patterns", async () => {
    const { router } = setup();
    await router.goto("/a/special");
    expect(router.outlet()).toBe("a:special");
  });

  it("falls back to * for unknown and deep paths", async () => {
    const { router } = setup();
    await router.goto("/nope");
    expect(router.outlet()).toBe("404");
    await router.goto("/a/b/c/d/e");
    expect(router.outlet()).toBe("404");
  });

  it("does not throw on percent-encoded paths", async () => {
    const { router } = setup();
    await router.goto("/%E0%A4%A");
    expect(router.outlet()).toBe("404");
    await router.goto("/a/%2F%20x");
    expect(router.outlet()).toBe("a:%2F%20x");
  });

  it("throws when no route matches", async () => {
    const { router } = setup([{ path: "/", render: () => "home" }]);
    await expect(router.goto("/x")).rejects.toThrow("No route found for /x");
  });

  it("passes params to render and enter", async () => {
    const enter = vi.fn<(p: Params) => boolean>(() => true);
    const { router } = setup(routes({ "/a/:id": enter }));
    await router.goto("/a/foo");
    expect(enter).toHaveBeenCalledWith({ id: "foo" });
    expect(router.outlet()).toBe("a:foo");
    await router.goto("/a/x/downloading");
    expect(router.outlet()).toBe("a:x:downloading");
  });

  it("compiles patterns lazily", () => {
    const spy = vi.spyOn(globalThis, "URLPattern");
    setup();
    expect(spy).not.toHaveBeenCalled();
  });
});

describe("enter", () => {
  it("updates on true", async () => {
    const { router, host } = setup(routes({ "/": () => true }));
    await router.goto("/");
    expect(router.outlet()).toBe("home");
    expect(host.updates).toBe(1);
  });

  it("treats undefined as success", async () => {
    const { router, host } = setup(routes({ "/": (() => undefined) as unknown as () => boolean }));
    await router.goto("/");
    expect(router.outlet()).toBe("home");
    expect(host.updates).toBe(1);
  });

  it("cancels on false without state change", async () => {
    const { router, host } = setup(routes({ "/a/:id": async () => false }));
    await router.goto("/");
    const updates = host.updates;
    await router.goto("/a/1");
    expect(router.outlet()).toBe("home");
    expect(host.updates).toBe(updates);
  });
});

describe("redirect", () => {
  it("replaces the URL without growing history", async () => {
    const { router } = setup();
    const len = window.history.length;
    await router.redirect("/a/1");
    expect(window.location.pathname).toBe("/a/1");
    expect(window.history.length).toBe(len);
    expect(router.outlet()).toBe("a:1");
  });
});

describe("stale navigation", () => {
  it("newer navigation wins over a slower earlier one", async () => {
    let release!: (v: boolean) => void;
    const slow = new Promise<boolean>((r) => (release = r));
    const { router, host } = setup(routes({ "/a/:id": () => slow }));
    const first = router.goto("/a/1");
    await router.goto("/");
    expect(router.outlet()).toBe("home");
    release(true);
    await first;
    expect(router.outlet()).toBe("home");
    expect(host.updates).toBe(1);
  });
});

describe("click interception", () => {
  let router: Router;
  let host: Host;
  beforeEach(async () => {
    ({ router, host } = setup());
    host.connect();
    await tick();
  });
  afterEach(() => host.disconnect());

  it("kicks off the current location on connect", () => {
    expect(router.outlet()).toBe("home");
  });

  it("intercepts same-origin clicks: pushState and goto", async () => {
    const len = window.history.length;
    const ev = clickOn(anchor("/a/9"));
    expect(ev.defaultPrevented).toBe(true);
    await tick();
    expect(window.location.pathname).toBe("/a/9");
    expect(window.history.length).toBe(len + 1);
    expect(router.outlet()).toBe("a:9");
  });

  it("ignores the query string when matching", async () => {
    clickOn(anchor("/a/9?page=2"));
    await tick();
    expect(window.location.search).toBe("?page=2");
    expect(router.outlet()).toBe("a:9");
  });

  it("prevents default but does not push for the same URL", async () => {
    window.history.replaceState({}, "", "/a/9");
    const len = window.history.length;
    const ev = clickOn(anchor("/a/9"));
    expect(ev.defaultPrevented).toBe(true);
    expect(window.history.length).toBe(len);
  });

  it("finds anchors inside shadow DOM via composedPath", async () => {
    const hostEl = document.createElement("div");
    document.body.appendChild(hostEl);
    const root = hostEl.attachShadow({ mode: "open" });
    const a = document.createElement("a");
    a.setAttribute("href", "/a/shadow");
    const span = document.createElement("span");
    a.appendChild(span);
    root.appendChild(a);
    const ev = clickOn(span);
    expect(ev.defaultPrevented).toBe(true);
    await tick();
    expect(router.outlet()).toBe("a:shadow");
  });

  const notIntercepted: [string, () => MouseEvent][] = [
    ["middle button", () => clickOn(anchor("/a/1"), { button: 1 })],
    ["meta", () => clickOn(anchor("/a/1"), { metaKey: true })],
    ["ctrl", () => clickOn(anchor("/a/1"), { ctrlKey: true })],
    ["shift", () => clickOn(anchor("/a/1"), { shiftKey: true })],
    ["alt", () => clickOn(anchor("/a/1"), { altKey: true })],
    ["target=_blank", () => clickOn(anchor("/a/1", { target: "_blank" }))],
    ["download", () => clickOn(anchor("/a/1", { download: "" }))],
    ["rel=external", () => clickOn(anchor("/a/1", { rel: "external" }))],
    ["mailto", () => clickOn(anchor("mailto:a@b.c"))],
    ["other origin", () => clickOn(anchor("https://example.com/a/1"))],
    ["no anchor", () => clickOn(document.body)],
  ];
  it.each(notIntercepted)("does not intercept: %s", async (_name, fire) => {
    const len = window.history.length;
    const ev = fire();
    expect(ev.defaultPrevented).toBe(false);
    await tick();
    expect(window.history.length).toBe(len);
    expect(router.outlet()).toBe("home");
  });

  it("does not intercept already-prevented clicks", () => {
    const a = anchor("/a/1");
    a.addEventListener("click", (e) => e.preventDefault());
    clickOn(a);
    expect(window.location.pathname).toBe("/");
  });
});

describe("popstate and lifecycle", () => {
  it("goes to the current location on popstate", async () => {
    const { router, host } = setup();
    host.connect();
    await tick();
    window.history.replaceState({}, "", "/a/pop");
    window.dispatchEvent(new PopStateEvent("popstate"));
    await tick();
    expect(router.outlet()).toBe("a:pop");
    host.disconnect();
  });

  it("removes listeners on disconnect", async () => {
    const { router, host } = setup();
    host.connect();
    await tick();
    host.disconnect();
    const ev = clickOn(anchor("/a/1"));
    expect(ev.defaultPrevented).toBe(false);
    window.history.replaceState({}, "", "/a/gone");
    window.dispatchEvent(new PopStateEvent("popstate"));
    await tick();
    expect(router.outlet()).toBe("home");
  });
});
