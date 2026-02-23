import { html, LitElement, unsafeCSS, css, type TemplateResult, type PropertyValues } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

import { type Category, fetchIndexerCategories } from '../utils/api';
import '../components/navbar.ts';
import '../components/resource_list.ts';
import { scrollableOnScroll } from '../utils/scroll_to_top';
import globalStyles from '/src/index.css?inline';

@customElement('indexer-view')
export class IndexerView extends LitElement {
  static styles = [
    unsafeCSS(globalStyles),
    css`
      #left-panel-categories li ul {
        margin-inline-start: 0.75rem;
        padding-left: 0;
      }
      #left-panel-categories li a {
        padding-left: 0.5rem;
      }

      body::-webkit-scrollbar {
        width: 8px;
      }

      body::-webkit-scrollbar-track {
        background: #1f2937;
      }

      body::-webkit-scrollbar-thumb {
        background-color: #4b5563;
        border-radius: 20px;
        border: 2px solid #1f2937;
      }

      /* To prevent items from splitting across columns */
      .break-inside-avoid-column {
        break-inside: avoid-column;
      }
    `,
  ];

  @property({ type: String })
  public indexerId: string = '';

  @state()
  private categories: Category[] = [];

  @property({ type: String })
  public category: string = '';

  @property({ type: Number })
  public page: number = 1;

  @state()
  private sidebarVisible: boolean = true;

  private handleSidebarToggle(): void {
    this.sidebarVisible = !this.sidebarVisible;
  }

  private renderCategory(category: Category): TemplateResult {
    const isActive = this.category === category.id;
    const activeClass = isActive ? 'menu-active' : '';

    if (category.subCategories && category.subCategories.length > 0) {
      return html`
        <li>
          <a class="${activeClass}" href="/indexers/${this.indexerId}/${category.id}">${category.name}</a>
          <ul>
            ${category.subCategories.map((child) => this.renderCategory(child))}
          </ul>
        </li>
      `;
    } else {
      return html`<li>
        <a class="${activeClass}" href="/indexers/${this.indexerId}/${category.id}">${category.name}</a>
      </li> `;
    }
  }

  async connectedCallback() {
    super.connectedCallback();
    await this.fetchIndexerCategories();
  }

  private findCategoryName(id: string, categories: Category[]): string | null {
    for (const category of categories) {
      if (category.id === id) {
        return category.name;
      }
      if (category.subCategories) {
        const found = this.findCategoryName(id, category.subCategories);
        if (found) {
          return found;
        }
      }
    }
    return null;
  }

  protected async update(changedProperties: PropertyValues): Promise<void> {
    if (changedProperties.has('indexerId')) {
      await this.fetchIndexerCategories();
    }

    if (this.indexerId) {
      let title = 'AutoGet - ' + this.indexerId;
      if (this.category && this.categories.length > 0) {
        const categoryName = this.findCategoryName(this.category, this.categories);
        if (categoryName) {
          title += ` - ${categoryName}`;
        }
      }
      title += ` - Page ${this.page}`;
      document.title = title;
    }

    super.update(changedProperties);
  }

  private async fetchIndexerCategories() {
    this.categories = await fetchIndexerCategories(this.indexerId);
  }

  render() {
    const sidebarClass = this.sidebarVisible ? 'flex-2' : 'w-0';
    return html`
      <div class="flex flex-col h-screen" @sidebar-toggle=${this.handleSidebarToggle}>
        <app-navbar .activePage=${this.indexerId} .sidebarVisible=${this.sidebarVisible}></app-navbar>

        <div class="flex flex-row grow overflow-hidden">
          <div
            class="${sidebarClass} bg-base-200 overflow-y-auto transition-all duration-300 ease-in-out ${this
              .sidebarVisible
              ? ''
              : 'opacity-0'}"
            id="left-panel-categories"
          >
            <ul class="menu bg-base-200 rounded-box w-full">
              ${this.categories.map((category) => this.renderCategory(category))}
            </ul>
          </div>

          <div
            class="flex-10 p-4 overflow-y-auto"
            id="content"
            @scroll=${(e: Event) => scrollableOnScroll(e.currentTarget as HTMLElement)}
          >
            <resource-list .indexerId=${this.indexerId} .category=${this.category} .page=${this.page}></resource-list>
          </div>
        </div>
      </div>
    `;
  }
}
