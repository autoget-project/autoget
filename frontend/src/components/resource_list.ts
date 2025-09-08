import { html, LitElement, unsafeCSS, css, type TemplateResult, type PropertyValues } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { DateTime } from 'luxon';

import { fetchIndexerResources, type ResourcesResponse } from '../utils/api';
import { formatBytes, formatCreatedDate } from '../utils/format';
import globalStyles from '/src/index.css?inline';

@customElement('resource-list')
export class ResourceList extends LitElement {
  static styles = [unsafeCSS(globalStyles), css``];

  @property({ type: String })
  public indexerId: string = '';

  @property({ type: String })
  public category: string = '';

  @property({ type: String })
  public keyword: string = '';

  @property({ type: Number })
  public page: number = 1;

  @state()
  private resources: ResourcesResponse | null = null;

  @state()
  private totalPages: number = 1;

  @state()
  private columnCount: number = 1; // Default to 1 column

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener('resize', this.handleResize);
    this.handleResize(); // Initial call to set column count
  }

  disconnectedCallback() {
    window.removeEventListener('resize', this.handleResize);
    super.disconnectedCallback();
  }

  private handleResize = () => {
    const width = window.innerWidth;
    if (width >= 1280) {
      // xl
      this.columnCount = 5;
    } else if (width >= 1024) {
      // lg
      this.columnCount = 4;
    } else if (width >= 768) {
      // md
      this.columnCount = 3;
    } else if (width >= 640) {
      // sm
      this.columnCount = 2;
    } else {
      this.columnCount = 1;
    }
  };

  protected async update(changedProperties: PropertyValues): Promise<void> {
    super.update(changedProperties);

    if (
      changedProperties.has('indexerId') ||
      changedProperties.has('category') ||
      changedProperties.has('keyword') ||
      changedProperties.has('page')
    ) {
      await this.fetchIndexerResources();
    }
  }

  private async fetchIndexerResources() {
    if (this.indexerId) {
      const response = await fetchIndexerResources(this.indexerId, this.category, this.keyword, this.page);
      if (response) {
        this.resources = response;
        this.totalPages = response.pagination.totalPages;
      } else {
        this.resources = null;
        this.totalPages = 1;
      }
    } else {
      this.resources = null;
      this.totalPages = 1;
    }
  }

  private handlePageChange(page: number) {
    if (page >= 1 && page <= this.totalPages) {
      const url = new URL(window.location.href);
      url.searchParams.set('page', page.toString());
      window.history.pushState({}, '', url.toString());
      window.dispatchEvent(new PopStateEvent('popstate'));
    }
  }

  private async handleDownloadClick(indexerId: string, resourceId: string) {
    const url = `/api/v1/indexers/${indexerId}/resources/${resourceId}/download`;
    try {
      const response = await fetch(url, { method: 'GET' });
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }
    } catch (error) {
      console.error('Error initiating download:', error);
    }
  }

  private renderResourceCard(resource: any): TemplateResult {
    return html`
      <div class="image-card rounded-lg overflow-hidden shadow-lg border border-gray-700 bg-gray-100 mb-2">
        ${resource.images && resource.images.length > 0
          ? html`<img
              src="${resource.images[0]}"
              alt="${resource.title || 'Resource image'}"
              class="w-full h-auto object-cover rounded-lg"
              loading="lazy"
            />`
          : ''}
        <div class="p-2">
          <h3 class="text font-medium line-clamp-4 text-balance break-all border-b-1 border-b-gray-400">
            ${resource.title || 'Untitled Resource'}
          </h3>
          ${resource.title2
            ? html`<p class="text font-normal line-clamp-4 text-balance break-all border-b-1 border-b-gray-400">
                ${resource.title2}
              </p>`
            : ''}
          <div class="flex flex-wrap gap-1 mt-1 mb-1 pb-1 border-b-1 border-b-gray-400">
            <span class="badge badge-outline badge-primary">${resource.category}</span>
            <span class="badge badge-outline badge-secondary">${formatBytes(resource.size)}</span>
            ${resource.resolution
              ? html`<span class="badge badge-outline badge-info">${resource.resolution}</span>`
              : ''}
            ${resource.free ? html`<span class="badge badge-success">Free</span>` : ''}
            <span
              class="badge ${DateTime.now().diff(DateTime.fromSeconds(resource.createdDate, { zone: 'utc' }), 'weeks')
                .weeks < 1
                ? 'badge-accent'
                : 'badge-neutral'}"
            >
              <span class="icon-[mingcute--time-line]"></span>
              ${formatCreatedDate(resource.createdDate)}
            </span>
            <span class="badge badge-info">
              <span class="icon-[icons8--up-round]"></span>
              ${resource.seeders}
            </span>
          </div>
          ${resource.labels && resource.labels.length > 0
            ? html` <div class="flex flex-wrap gap-1 mt-1 mb-1 pb-1 border-b-1 border-b-gray-400">
                ${resource.labels.map(
                  (label: string) => html` <span class="badge badge-outline badge-neutral">${label}</span> `,
                )}
              </div>`
            : ''}
          <div class="flex flex-row basis-full justify-end">
            <button class="btn btn-xs btn-info" @click=${() => this.handleDownloadClick(this.indexerId, resource.id)}>
              Download
            </button>
          </div>
        </div>
      </div>
    `;
  }

  private renderColumns(): TemplateResult {
    if (!this.resources || !this.resources.resources || this.resources.resources.length === 0) {
      return html`<p>No resources found or loading...</p>`;
    }

    const columns: TemplateResult[][] = Array.from({ length: this.columnCount }, () => []);
    this.resources.resources.forEach((resource, index) => {
      const columnIndex = index % this.columnCount; // Distribute items in row-first order
      columns[columnIndex].push(this.renderResourceCard(resource));
    });

    return html`
      <div class="columns-2 sm:columns-2 md:columns-3 lg:columns-4 xl:columns-5 gap-2">
        ${columns.map((colItems) => html` <div class="flex flex-col flex-grow gap-2">${colItems}</div> `)}
      </div>
    `;
  }

  private renderPagination(): TemplateResult | null {
    if (this.totalPages <= 1) {
      return null;
    }

    const pages: (number | string)[] = [];
    const maxPagesToShow = 5;
    const half = Math.floor(maxPagesToShow / 2);

    let startPage = Math.max(1, this.page - half);
    let endPage = Math.min(this.totalPages, this.page + half);

    if (endPage - startPage + 1 < maxPagesToShow) {
      if (this.page <= half) {
        endPage = Math.min(this.totalPages, maxPagesToShow);
      } else if (this.page + half >= this.totalPages) {
        startPage = Math.max(1, this.totalPages - maxPagesToShow + 1);
      }
    }

    if (startPage > 1) {
      pages.push('<');
    }

    for (let i = startPage; i <= endPage; i++) {
      pages.push(i);
    }

    if (endPage < this.totalPages) {
      pages.push('>');
    }

    return html`
      <div class="flex justify-center my-4">
        <div class="join">
          ${pages.map((page) => {
            const isActive = page === this.page;
            const isDisabled = (page === '<' && this.page === 1) || (page === '>' && this.page === this.totalPages);
            const buttonClass = `join-item btn ${isActive ? 'btn-active' : ''} ${isDisabled ? 'btn-disabled' : ''}`;

            if (typeof page === 'number') {
              return html`<button class="${buttonClass}" @click=${() => this.handlePageChange(page)}>${page}</button>`;
            } else if (page === '<') {
              return html`<button class="${buttonClass}" @click=${() => this.handlePageChange(this.page - 1)}>
                &laquo;
              </button>`;
            } else if (page === '>') {
              return html`<button class="${buttonClass}" @click=${() => this.handlePageChange(this.page + 1)}>
                &raquo;
              </button>`;
            }
            return null;
          })}
        </div>
      </div>
    `;
  }

  render() {
    return html`
      ${this.renderPagination()}
      <div id="masonry-container">${this.renderColumns()}</div>
      ${this.renderPagination()}
    `;
  }
}
