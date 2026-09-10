import { LitElement, html, unsafeCSS } from "lit";
import { customElement, property, state } from "lit/decorators.js";

import { fetchIndexers, fetchDownloaders, type DownloaderInfo } from "../utils/api";
import globalStyles from "/src/index.css?inline";

@customElement("app-navbar")
export class AppNavbar extends LitElement {
  static styles = [unsafeCSS(globalStyles)];

  @state()
  private indexers: string[] = [];

  @state()
  private downloaders: DownloaderInfo[] = [];

  @property({ type: String })
  activePage = "";

  @property({ type: Boolean })
  sidebarVisible = true;

  private toggleSidebar() {
    this.dispatchEvent(
      new CustomEvent("sidebar-toggle", {
        bubbles: true,
        composed: true,
      }),
    );
  }

  private refreshTimer: ReturnType<typeof setInterval> | null = null;
  private readonly refreshInterval = 20000; // 20 seconds

  async connectedCallback() {
    super.connectedCallback();
    this.indexers = await fetchIndexers();
    this.downloaders = await fetchDownloaders();
    this.startRefreshTimer();
    this.requestUpdate();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    this.stopRefreshTimer();
  }

  private startRefreshTimer() {
    this.stopRefreshTimer(); // Clear any existing timer
    this.refreshTimer = setInterval(() => {
      this.loadDownloaders();
    }, this.refreshInterval);
  }

  private stopRefreshTimer() {
    if (this.refreshTimer !== null) {
      clearInterval(this.refreshTimer);
      this.refreshTimer = null;
    }
  }

  private async loadDownloaders() {
    try {
      this.downloaders = await fetchDownloaders();
    } catch (error) {
      console.error("Failed to refresh downloaders:", error);
    }
  }

  private getBorderColor(downloader: DownloaderInfo): string {
    // Priority: failed > planned > downloading
    if (downloader.count_of_failed > 0) {
      return "border-error"; // Red
    } else if (downloader.count_of_planned > 0) {
      return "border-info"; // Blue
    } else if (downloader.count_of_downloading > 0) {
      return "border-success"; // Green
    }
    return "";
  }

  private getSummaryBadgeColor(): string {
    if (this.downloaders.some((d) => d.count_of_failed > 0)) {
      return "badge-error";
    }
    if (this.downloaders.some((d) => d.count_of_planned > 0)) {
      return "badge-info";
    }
    if (this.downloaders.some((d) => d.count_of_downloading > 0)) {
      return "badge-success";
    }
    return "";
  }

  render() {
    const isIndexerPage = this.indexers.includes(this.activePage);
    const summaryBadgeColor = this.getSummaryBadgeColor();

    return html`
      <div class="navbar bg-base-200 px-2 sm:px-4 min-h-14">
        <!-- Left part: Toggle, Logo, and Indexer Tabs -->
        <div class="flex items-center gap-1 min-w-0 flex-1">
          ${
            isIndexerPage
              ? html`
                  <button
                    class="btn btn-square btn-ghost btn-sm sm:btn-md shrink-0"
                    aria-label="Toggle categories"
                    @click=${this.toggleSidebar}
                  >
                    <span
                      class="icon-[mdi--chevron-left] w-6 h-6 sm:w-8 sm:h-8 transition-transform duration-300 ${
                        this.sidebarVisible ? "" : "rotate-180"
                      }"
                    ></span>
                  </button>
                `
              : ""
          }
          <a href="/" class="btn btn-square btn-ghost btn-sm sm:btn-md shrink-0" aria-label="Home">
            <img src="/icon.svg" alt="Icon" class="w-6 h-6 sm:w-8 sm:h-8" />
          </a>
          <!-- Indexers scrollable tabs container -->
          <div class="overflow-x-auto min-w-0 flex items-center scrollbar-none">
            <div role="tablist" class="tabs tabs-border flex-nowrap whitespace-nowrap">
              ${this.indexers.map((indexer) => {
                const isActive = this.activePage === indexer;
                return html`<a
                  href="/indexers/${indexer}"
                  class="tab tab-sm sm:tab-md ${isActive ? "tab-active font-bold" : ""}"
                  role="tab"
                  >${indexer}</a
                >`;
              })}
            </div>
          </div>
        </div>

        <!-- Right part: Desktop links vs Mobile Dropdown -->
        <div class="shrink-0 flex items-center gap-1 ml-2">
          <!-- Desktop navigation links -->
          <div class="hidden md:flex items-center gap-2">
            ${this.downloaders.map((downloader) => {
              const isActive = this.activePage === downloader.name;
              const borderColor = this.getBorderColor(downloader);

              return html`
                <a
                  href="/downloaders/${downloader.name}"
                  class="btn btn-ghost btn-sm ${isActive ? "btn-active" : ""} border-2 ${borderColor}"
                >
                  ${downloader.name}
                </a>
              `;
            })}
            <a
              href="/search"
              class="btn btn-ghost btn-sm ${this.activePage === "search" ? "btn-active" : ""}"
              >Search</a
            >
          </div>

          <!-- Mobile navigation dropdown -->
          <div class="dropdown dropdown-end md:hidden">
            <div
              tabindex="0"
              role="button"
              class="btn btn-ghost btn-sm btn-square relative"
              aria-label="Menu"
            >
              <span class="icon-[heroicons--bars-3] w-6 h-6"></span>
              ${
                summaryBadgeColor
                  ? html`<span
                      class="badge badge-xs ${summaryBadgeColor} absolute top-1 right-1"
                    ></span>`
                  : ""
              }
            </div>
            <ul
              tabindex="0"
              class="menu dropdown-content bg-base-100 rounded-box z-50 mt-2 w-52 p-2 shadow-xl border border-base-300"
            >
              <li class="menu-title text-xs">Search</li>
              <li>
                <a
                  href="/search"
                  class="${this.activePage === "search" ? "menu-active font-bold" : ""}"
                >
                  <span class="icon-[material-symbols--search] w-5 h-5"></span>
                  Search
                </a>
              </li>
              ${
                this.downloaders.length > 0
                  ? html`
                      <li class="menu-title text-xs mt-2">Downloaders</li>
                      ${this.downloaders.map((downloader) => {
                        const isActive = this.activePage === downloader.name;
                        const borderColor = this.getBorderColor(downloader);
                        return html`
                          <li>
                            <a
                              href="/downloaders/${downloader.name}"
                              class="flex items-center justify-between ${
                                isActive ? "menu-active font-bold" : ""
                              }"
                            >
                              <span>${downloader.name}</span>
                              ${
                                borderColor
                                  ? html`<span
                                      class="badge badge-xs ${borderColor.replace(
                                        "border-",
                                        "badge-",
                                      )}"
                                    ></span>`
                                  : ""
                              }
                            </a>
                          </li>
                        `;
                      })}
                    `
                  : ""
              }
            </ul>
          </div>
        </div>
      </div>
    `;
  }
}
