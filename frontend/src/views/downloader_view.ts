import { html, LitElement, unsafeCSS } from 'lit';
import { customElement, property } from 'lit/decorators.js';

import '../components/navbar.ts';
import globalStyles from '/src/index.css?inline';

@customElement('downloader-view')
export class DownloaderView extends LitElement {
  static styles = [unsafeCSS(globalStyles)];

  @property({ type: String })
  public downloaderId: string = '';

  render() {
    return html`
      <div class="flex flex-col h-screen">
        <app-navbar .activePage=${this.downloaderId}></app-navbar>

        <div class="flex flex-row flex-grow overflow-hidden">
          <div class="flex-10 p-4 overflow-y-auto">
            <div class="flex items-center justify-center h-full">
              <div class="text-center">
                <h1 class="text-2xl font-bold mb-4">Downloader: ${this.downloaderId}</h1>
                <p class="text-base-content/70">Downloader view content will be implemented here.</p>
              </div>
            </div>
          </div>
        </div>
      </div>
    `;
  }
}
