import { html, LitElement } from "lit";
import { customElement } from "lit/decorators.js";

import { initTheme } from "./utils/theme.ts";

@customElement("app-root")
export class App extends LitElement {
  connectedCallback() {
    super.connectedCallback();
    initTheme();
  }

  render() {
    return html` <app-router></app-router> `;
  }
}

// Safari < 26 and Firefox < 142 lack URLPattern; load the polyfill only there,
// and only then load the router, which needs it.
async function ensureUrlPattern(): Promise<void> {
  if (!("URLPattern" in globalThis)) {
    await import("urlpattern-polyfill");
  }
}

void ensureUrlPattern().then(() => import("./router.ts"));
