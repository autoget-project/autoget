import { LitElement, html, unsafeCSS } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

import { fetchIndexers, fetchDownloaders } from '../utils/api';
import globalStyles from '/src/index.css?inline';

@customElement('app-navbar')
export class AppNavbar extends LitElement {
  static styles = [unsafeCSS(globalStyles)];

  @state()
  private indexers: string[] = [];

  @state()
  private downloaders: string[] = [];

  @property({ type: String })
  activePage = '';

  async connectedCallback() {
    super.connectedCallback();
    this.indexers = await fetchIndexers();
    this.downloaders = await fetchDownloaders();
  }

  render() {
    return html`
      <div class="navbar bg-base-200">
        <div class="navbar-start">
          <a href="/" class="btn-ghost">
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
              const isActive = this.activePage === downloader;
              return html`<a href="/downloaders/${downloader}" class="btn btn-ghost ${isActive ? 'btn-active' : ''}"
                >${downloader}</a
              >`;
            })}
            <a href="/search" class="btn btn-ghost ${this.activePage === 'search' ? 'btn-active' : ''}">Search</a>
          </div>
        </div>
      </div>
    `;
  }
}
