import { Router } from '@lit-labs/router';
import { LitElement, html } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { fetchIndexers, fetchIndexerCategories, fetchDownloaders, type DownloaderInfo } from './utils/api.ts';

import './views/search_view';
import './views/indexer_view';
import './views/downloader_view';
import './views/404_view';

@customElement('app-router')
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

  private async fetchIndexers() {
    this.indexers = await fetchIndexers();
  }

  private async fetchDownloaders() {
    this.downloaders = await fetchDownloaders();
  }

  private router = new Router(this, [
    {
      path: '/',
      render: () => html`<div>Loading...</div>`,
      enter: async () => {
        if (this.indexers.length === 0) {
          await this.fetchIndexers();
        }
        const newUrl = `/indexers/${this.indexers[0]}`;
        this.router.goto(newUrl);
        history.replaceState(null, '', newUrl);
        return false;
      },
    },
    { path: '/search', render: () => html`<search-view></search-view>` },
    {
      path: '/indexers/:id',
      render: ({ id }) => {
        return html`<indexer-view .indexerId=${id || ''} category=""></indexer-view>`;
      },
      enter: async ({ id }) => {
        if (this.indexers.length === 0) {
          await this.fetchIndexers();
        }
        if (id === undefined || !this.indexers.includes(id)) {
          this.router.goto('/404');
          return false;
        }
        const categories = await fetchIndexerCategories(id);
        this.router.goto(`/indexers/${id}/${categories[0].id}`);
        history.replaceState(null, '', `/indexers/${id}/${categories[0].id}`);
        return false;
      },
    },
    {
      path: '/indexers/:id/:category',
      render: ({ id, category }) => {
        const urlParams = new URLSearchParams(window.location.search);
        const page = urlParams.get('page');
        return html`<indexer-view
          .indexerId=${id || ''}
          .category=${category || ''}
          .page=${page ? Number(page) : 1}
        ></indexer-view>`;
      },
    },
    {
      path: '/downloaders/:id',
      render: ({ id }) => {
        return html`<downloader-view .downloaderId=${id || ''}></downloader-view>`;
      },
      enter: async ({ id }) => {
        if (this.downloaders.length === 0) {
          await this.fetchDownloaders();
        }
        const downloaderNames = this.downloaders.map((d) => d.name);
        if (id === undefined || !downloaderNames.includes(id)) {
          this.router.goto('/404');
          return false;
        }
        // Redirect to default tab if no tab specified
        this.router.goto(`/downloaders/${id}/downloading`);
        history.replaceState(null, '', `/downloaders/${id}/downloading`);
        return false;
      },
    },
    {
      path: '/downloaders/:id/:tab',
      render: ({ id, tab }) => {
        return html`<downloader-view .downloaderId=${id || ''} .activeTab=${tab || ''}></downloader-view>`;
      },
      enter: async ({ id, tab }) => {
        if (this.downloaders.length === 0) {
          await this.fetchDownloaders();
        }
        const downloaderNames = this.downloaders.map((d) => d.name);
        if (id === undefined || !downloaderNames.includes(id)) {
          this.router.goto('/404');
          return false;
        }
        // Validate tab parameter
        const validTabs = ['downloading', 'seeding', 'stopped', 'planned', 'failed'];
        if (tab === undefined || !validTabs.includes(tab)) {
          this.router.goto(`/downloaders/${id}/downloading`);
          return false;
        }
        return true;
      },
    },
    { path: '*', render: () => html`<not-found-view></not-found-view>` },
  ]);

  render() {
    return this.router.outlet();
  }
}
