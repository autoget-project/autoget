import type { ReactiveController, ReactiveControllerHost } from "lit";

export type Params = Record<string, string | undefined>;

export interface RouteConfig {
  path: string;
  render: (params: Params) => unknown;
  // Returning exactly false cancels the navigation (no state change, no re-render).
  enter?: (params: Params) => boolean | Promise<boolean>;
}

// Minimal client-side router on native URLPattern (polyfilled on demand by app.ts).
// Patterns are compiled lazily on the first goto so load order cannot break it.
export class Router implements ReactiveController {
  private host: ReactiveControllerHost;
  private routes: RouteConfig[];
  private patterns: URLPattern[] | undefined;
  private currentRoute: RouteConfig | undefined;
  private currentParams: Params = {};
  private navId = 0;

  constructor(host: ReactiveControllerHost, routes: RouteConfig[]) {
    this.host = host;
    this.routes = [...routes];
    host.addController(this);
  }

  hostConnected() {
    window.addEventListener("click", this.onClick);
    window.addEventListener("popstate", this.onPopState);
    this.goto(window.location.pathname).catch(console.error);
  }

  hostDisconnected() {
    window.removeEventListener("click", this.onClick);
    window.removeEventListener("popstate", this.onPopState);
  }

  async goto(pathname: string): Promise<void> {
    const id = ++this.navId;
    this.patterns ??= this.routes.map((r) => new URLPattern({ pathname: r.path }));
    const index = this.patterns.findIndex((p) => p.test({ pathname }));
    if (index === -1) {
      throw new Error(`No route found for ${pathname}`);
    }
    const route = this.routes[index];
    const params: Params = this.patterns[index].exec({ pathname })?.pathname.groups ?? {};
    if (route.enter) {
      const ok = await route.enter(params);
      if (ok === false || id !== this.navId) {
        return;
      }
    }
    this.currentRoute = route;
    this.currentParams = params;
    this.host.requestUpdate();
  }

  // Navigate without adding a history entry, so the URL bar shows the real route.
  redirect(path: string): Promise<void> {
    history.replaceState(null, "", path);
    return this.goto(path);
  }

  outlet(): unknown {
    return this.currentRoute?.render(this.currentParams);
  }

  private onClick = (e: MouseEvent) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) {
      return;
    }
    const anchor = e.composedPath().find((n) => (n as Element).tagName === "A") as
      | HTMLAnchorElement
      | undefined;
    if (
      anchor === undefined ||
      anchor.target !== "" ||
      anchor.hasAttribute("download") ||
      anchor.getAttribute("rel") === "external"
    ) {
      return;
    }
    const href = anchor.href;
    if (href === "" || href.startsWith("mailto:") || anchor.origin !== window.location.origin) {
      return;
    }
    e.preventDefault();
    if (href !== window.location.href) {
      window.history.pushState({}, "", href);
      this.goto(anchor.pathname).catch(console.error);
    }
  };

  private onPopState = () => {
    this.goto(window.location.pathname).catch(console.error);
  };
}
