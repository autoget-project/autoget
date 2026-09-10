import "urlpattern-polyfill";
import { html, LitElement } from "lit";
import { customElement } from "lit/decorators.js";

import { initTheme } from "./utils/theme.ts";
import "./router.ts";

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
