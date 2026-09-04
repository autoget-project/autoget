import { LitElement, html, unsafeCSS } from "lit";
import { customElement } from "lit/decorators.js";

import globalStyles from "/src/index.css?inline";

@customElement("not-found-view")
export class NotFoundView extends LitElement {
  static styles = [unsafeCSS(globalStyles)];

  render() {
    return html`
      <div class="hero bg-base-200 min-h-screen">
        <div class="hero-content text-center">
          <div class="max-w-md">
            <h1 class="text-5xl font-bold">404</h1>
            <p class="py-6 text-xl">Page Not Found</p>
            <a href="/" class="btn btn-primary">Go Home</a>
          </div>
        </div>
      </div>
    `;
  }
}
