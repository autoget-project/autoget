import { Router } from "@lit-labs/router";
import { LitElement, html } from "lit";
import { customElement, state } from "lit/decorators.js";
import {
  fetchIndexers,
  fetchIndexerCategories,
  fetchDownloaders,
  type DownloaderInfo,
} from "./utils/api.ts";
import { defaultCategoryId, resolveIndexers } from "./utils/router_helpers.ts";

import "./views/search_view";
import "./views/indexer_view";
import "./views/downloader_view";
import "./views/404_view";

@customElement("app-router")
export class AppRouter extends LitElement {
  @state()
  private indexers: string[] = [];

  @state()
  private downloaders: DownloaderInfo[] = [];

  async connectedCallback() {
    super.connectedCallback();
    this.fetchIndexers();
    this.fetchDownloaders();
  }

  @state()
  private loadFailed = false;

  private async ensureIndexers(): Promise<string[]> {
    if (this.indexers.length === 0) {
      this.indexers = await resolveIndexers(fetchIndexers);
    }
    return this.indexers;
  }

  @state()
  private loadMessage = "No indexers available. Check configuration or backend, then retry.";

  private retryPath = "/";

  // loadFailed stays set until a retry succeeds, so the failure view does not flash away.
  private retryLoad = () => {
    this.router.goto(this.retryPath);
  };

  private renderLoadFailure() {
    return html`<div class="p-8 text-center">
      <p class="mb-4">${this.loadMessage}</p>
      <button class="btn btn-primary" @click=${this.retryLoad}>Retry</button>
    </div>`;
  }

  private showLoadFailure(message: string, retryPath: string) {
    this.loadMessage = message;
    this.retryPath = retryPath;
    this.loadFailed = true;
  }

  private async fetchIndexers() {
    try {
      this.indexers = await fetchIndexers();
    } catch (error) {
      console.warn("Failed to fetch indexers:", error);
    }
  }

  // Returns false when the downloader list could not be loaded.
  private async fetchDownloaders(): Promise<boolean> {
    try {
      this.downloaders = await fetchDownloaders();
      return true;
    } catch (error) {
      console.warn("Failed to fetch downloaders:", error);
      return false;
    }
  }

  private router = new Router(this, [
    {
      path: "/",
      render: () => (this.loadFailed ? this.renderLoadFailure() : html`<div>Loading...</div>`),
      enter: async () => {
        const indexers = await this.ensureIndexers();
        if (indexers.length === 0) {
          this.showLoadFailure(
            "No indexers available. Check configuration or backend, then retry.",
            "/",
          );
          return true;
        }
        this.loadFailed = false;
        const newUrl = `/indexers/${encodeURIComponent(indexers[0])}`;
        this.router.goto(newUrl);
        history.replaceState(null, "", newUrl);
        return false;
      },
    },
    { path: "/search", render: () => html`<search-view></search-view>` },
    {
      path: "/indexers/:id",
      render: ({ id }) => {
        if (this.loadFailed) {
          return this.renderLoadFailure();
        }
        return html`<indexer-view .indexerId=${id || ""} category=""></indexer-view>`;
      },
      enter: async ({ id }) => {
        const indexers = await this.ensureIndexers();
        if (indexers.length === 0) {
          this.showLoadFailure(
            "No indexers available. Check configuration or backend, then retry.",
            "/",
          );
          this.router.goto("/");
          return false;
        }
        if (id === undefined || !indexers.includes(id)) {
          this.router.goto("/404");
          return false;
        }
        let categories;
        try {
          categories = await fetchIndexerCategories(id);
        } catch (error) {
          console.warn("Failed to fetch categories:", error);
          this.showLoadFailure(
            `Failed to load categories for "${id}". Check the backend, then retry.`,
            `/indexers/${encodeURIComponent(id)}`,
          );
          return true;
        }
        this.loadFailed = false;
        const cid = defaultCategoryId(categories);
        if (!cid) {
          this.router.goto("/404");
          return false;
        }
        const newUrl = `/indexers/${encodeURIComponent(id)}/${encodeURIComponent(cid)}`;
        this.router.goto(newUrl);
        history.replaceState(null, "", newUrl);
        return false;
      },
    },
    {
      path: "/indexers/:id/:category",
      render: ({ id, category }) => {
        const urlParams = new URLSearchParams(window.location.search);
        const page = urlParams.get("page");
        return html`<indexer-view
          .indexerId=${id || ""}
          .category=${category || ""}
          .page=${page ? Number(page) : 1}
        ></indexer-view>`;
      },
    },
    {
      path: "/downloaders/:id",
      render: ({ id }) => {
        return html`<downloader-view .downloaderId=${id || ""}></downloader-view>`;
      },
      enter: async ({ id }) => {
        const loaded = this.downloaders.length > 0 || (await this.fetchDownloaders());
        const downloaderNames = this.downloaders.map((d) => d.name);
        // When the list could not be loaded, let the view render and report the error.
        if (id === undefined || (loaded && !downloaderNames.includes(id))) {
          this.router.goto("/404");
          return false;
        }
        // Redirect to default tab if no tab specified
        this.router.goto(`/downloaders/${id}/downloading`);
        history.replaceState(null, "", `/downloaders/${id}/downloading`);
        return false;
      },
    },
    {
      path: "/downloaders/:id/:tab",
      render: ({ id, tab }) => {
        return html`<downloader-view
          .downloaderId=${id || ""}
          .activeTab=${tab || ""}
        ></downloader-view>`;
      },
      enter: async ({ id, tab }) => {
        const loaded = this.downloaders.length > 0 || (await this.fetchDownloaders());
        const downloaderNames = this.downloaders.map((d) => d.name);
        // When the list could not be loaded, let the view render and report the error.
        if (id === undefined || (loaded && !downloaderNames.includes(id))) {
          this.router.goto("/404");
          return false;
        }
        // Validate tab parameter
        const validTabs = ["downloading", "seeding", "stopped", "planned", "failed"];
        if (tab === undefined || !validTabs.includes(tab)) {
          this.router.goto(`/downloaders/${id}/downloading`);
          return false;
        }
        return true;
      },
    },
    { path: "*", render: () => html`<not-found-view></not-found-view>` },
  ]);

  render() {
    return this.router.outlet();
  }
}
