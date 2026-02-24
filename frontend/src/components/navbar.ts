import { LitElement, html, unsafeCSS } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

import { fetchIndexers, fetchDownloaders, type DownloaderInfo } from '../utils/api';
import globalStyles from '/src/index.css?inline';

@customElement('app-navbar')
export class AppNavbar extends LitElement {
  static styles = [unsafeCSS(globalStyles)];

  @state()
  private indexers: string[] = [];

  @state()
  private downloaders: DownloaderInfo[] = [];

  @property({ type: String })
  activePage = '';

  @property({ type: Boolean })
  sidebarVisible = true;

  private toggleSidebar() {
    this.dispatchEvent(
      new CustomEvent('sidebar-toggle', {
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
      console.error('Failed to refresh downloaders:', error);
    }
  }

  private getBorderColor(downloader: DownloaderInfo): string {
    // Priority: failed > planned > downloading
    if (downloader.count_of_failed > 0) {
      return 'border-error'; // Red
    } else if (downloader.count_of_planned > 0) {
      return 'border-info'; // Blue
    } else if (downloader.count_of_downloading > 0) {
      return 'border-success'; // Green
    }
    return '';
  }

  render() {
    const isIndexerPage = this.indexers.includes(this.activePage);
    return html`
      <div class="navbar bg-base-200">
        <div class="navbar-start gap-1">
          ${isIndexerPage
            ? html`
                <a class="btn btn-square btn-ghost" @click=${this.toggleSidebar}>
                  <span
                    class="icon-[mdi--chevron-left] w-8 h-8 transition-transform duration-300 ${this.sidebarVisible
                      ? ''
                      : 'rotate-180'}"
                  ></span>
                </a>
              `
            : ''}
          <a href="/" class="btn btn-square btn-ghost">
            <img src="/icon.svg" alt="Icon" class="w-8 h-8" />
          </a>
          <div role="tablist" class="tabs tabs-border">
            ${this.indexers.map((indexer) => {
              const isActive = this.activePage === indexer;
              return html`<a href="/indexers/${indexer}" class="tab ${isActive ? 'tab-active' : ''}" role="tab"
                >${indexer}</a
              >`;
            })}
          </div>
        </div>
        <div class="navbar-end">
          <div class="flex gap-2">
            ${this.downloaders.map((downloader) => {
              const isActive = this.activePage === downloader.name;
              const borderColor = this.getBorderColor(downloader);

              return html`
                <a
                  href="/downloaders/${downloader.name}"
                  class="btn btn-ghost ${isActive ? 'btn-active' : ''} border-2 ${borderColor}"
                >
                  ${downloader.name}
                </a>
              `;
            })}
            <a href="/search" class="btn btn-ghost ${this.activePage === 'search' ? 'btn-active' : ''}">Search</a>
          </div>
        </div>
      </div>
    `;
  }
}
