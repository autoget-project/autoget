import { html, LitElement, unsafeCSS, css, type TemplateResult } from 'lit';
import { customElement, state } from 'lit/decorators.js';

import { fetchIndexers, fetchIndexerCategories, type Category } from '../utils/api';
import '../components/navbar.ts';
import '../components/resource_list.ts';
import { scrollableOnScroll } from '../utils/scroll_to_top';
import globalStyles from '/src/index.css?inline';

@customElement('search-view')
export class SearchView extends LitElement {
  static styles = [
    unsafeCSS(globalStyles),
    css`
      .category-item {
        cursor: pointer;
        transition:
          transform 0.2s,
          box-shadow 0.2s;
      }
      .category-item:hover {
        transform: translateY(-2px);
        box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
      }
      .category-item.active {
        background-color: #4f46e5;
        color: white;
        border-color: #4f46e5;
        transform: scale(1.02);
        box-shadow: 0 6px 12px rgba(0, 0, 0, 0.2);
      }
      .scroll-container {
        overflow-x: auto;
        scrollbar-width: none; /* Firefox */
        -ms-overflow-style: none; /* IE and Edge */
      }
      .scroll-container::-webkit-scrollbar {
        display: none; /* Chrome, Safari, and Opera */
      }
      .break-inside-avoid-column {
        break-inside: avoid-column;
      }
    `,
  ];

  @state()
  private indexers: string[] = [];

  @state()
  private selectedIndexer = '';

  @state()
  private allCategories: Category[] = []; // All categories fetched for the selected indexer

  @state()
  private displayedCategoryLevels: Category[][] = []; // Categories to display for each level

  @state()
  private selectedCategoryPath: Category[] = []; // Path of selected categories

  @state()
  private searchQuery = '';

  @state()
  private currentKeyword: string = '';

  @state()
  private currentIndexer: string = '';

  @state()
  private currentCategory: string = '';

  @state()
  private currentPage: number = 1;

  @state()
  private isSearching: boolean = false;

  async connectedCallback() {
    super.connectedCallback();
    this.indexers = await fetchIndexers();
    window.addEventListener('popstate', this.handlePopState);
    this.handlePopState(); // Initial call to set state from URL
  }

  disconnectedCallback() {
    window.removeEventListener('popstate', this.handlePopState);
    super.disconnectedCallback();
  }

  private handlePopState = () => {
    const urlParams = new URLSearchParams(window.location.search);
    this.currentKeyword = urlParams.get('keyword') || '';
    this.currentIndexer = urlParams.get('indexer') || '';
    this.currentCategory = urlParams.get('category') || '';
    this.currentPage = parseInt(urlParams.get('page') || '1', 10);

    // Update search query input if it's different from the URL
    if (this.searchQuery !== this.currentKeyword) {
      this.searchQuery = this.currentKeyword;
    }

    // If indexer changed from URL, re-fetch categories
    if (this.selectedIndexer !== this.currentIndexer) {
      this.selectedIndexer = this.currentIndexer;
      // Ensure allCategories is cleared if no indexer is selected
      if (!this.selectedIndexer) {
        this.allCategories = [];
      }
      this.fetchAndDisplayCategories();
    }

    // Update selected category path based on URL
    // This is a simplified approach; a more robust solution might involve re-building the path from allCategories
    if (this.currentCategory) {
      const categoryInPath = this.selectedCategoryPath.find((c) => c.id === this.currentCategory);
      if (!categoryInPath) {
        // Attempt to find the category in allCategories and set the path
        const findCategoryAndPath = (
          categories: Category[],
          targetId: string,
          currentPath: Category[],
        ): Category[] | null => {
          for (const cat of categories) {
            const newPath = [...currentPath, cat];
            if (cat.id === targetId) {
              return newPath;
            }
            if (cat.subCategories) {
              const subPath = findCategoryAndPath(cat.subCategories, targetId, newPath);
              if (subPath) {
                return subPath;
              }
            }
          }
          return null;
        };
        const newPath = findCategoryAndPath(this.allCategories, this.currentCategory, []);
        if (newPath) {
          this.selectedCategoryPath = newPath;
          // Also update displayedCategoryLevels to reflect the path
          this.displayedCategoryLevels = [];
          let currentLevelCategories = this.allCategories;
          for (let i = 0; i < newPath.length; i++) {
            this.displayedCategoryLevels.push(currentLevelCategories);
            currentLevelCategories = newPath[i].subCategories || [];
          }
        } else {
          this.selectedCategoryPath = [];
          this.displayedCategoryLevels = [this.allCategories];
        }
      }
    } else {
      this.selectedCategoryPath = [];
      // Only display top-level categories if an indexer is selected
      this.displayedCategoryLevels = this.selectedIndexer ? [this.allCategories] : [];
    }

    // Reset searching state when URL changes (indicating search completed)
    this.isSearching = false;
    this.requestUpdate();
  };

  private async handleIndexerChange(indexer: string) {
    this.selectedIndexer = indexer;
    this.selectedCategoryPath = [];
    await this.fetchAndDisplayCategories();
  }

  private async fetchAndDisplayCategories() {
    if (this.selectedIndexer) {
      this.allCategories = await fetchIndexerCategories(this.selectedIndexer);
      this.displayedCategoryLevels = [this.allCategories]; // Start with the top level
    } else {
      this.allCategories = [];
      this.displayedCategoryLevels = [];
    }
  }

  private handleSearch(e: Event) {
    e.preventDefault();

    // Set searching state to true when search is initiated
    this.isSearching = true;
    this.requestUpdate();

    const url = new URL(window.location.href);
    url.searchParams.set('keyword', this.searchQuery);
    url.searchParams.set('indexer', this.selectedIndexer);
    url.searchParams.set('page', '1');
    const categoryId = this.selectedCategoryPath[this.selectedCategoryPath.length - 1]?.id;
    if (categoryId) {
      url.searchParams.set('category', categoryId);
    } else {
      url.searchParams.delete('category');
    }
    window.history.pushState({}, '', url.toString());
    window.dispatchEvent(new PopStateEvent('popstate'));

    // Uncheck the search-collapse checkbox
    const searchCollapse = this.shadowRoot?.getElementById('search-collapse') as HTMLInputElement;
    if (searchCollapse) {
      searchCollapse.checked = false;
    }
  }

  private handleSearchQueryInput(e: Event) {
    this.searchQuery = (e.target as HTMLInputElement).value;
  }

  private handleCategoryClick(category: Category, level: number) {
    // Update the selected path
    this.selectedCategoryPath = this.selectedCategoryPath.slice(0, level);
    this.selectedCategoryPath.push(category);

    // Determine if there are subcategories
    if (category.subCategories && category.subCategories.length > 0) {
      // Add the next level of categories to display
      this.displayedCategoryLevels = this.displayedCategoryLevels.slice(0, level + 1);
      this.displayedCategoryLevels.push(category.subCategories);
    } else {
      // Hide subsequent levels if a leaf node is selected
      this.displayedCategoryLevels = this.displayedCategoryLevels.slice(0, level + 1);
    }
    this.requestUpdate(); // Force re-render to update active states and displayed levels
  }

  private renderCategoryLevel(categories: Category[], level: number): TemplateResult {
    const levelTitle = level === 0 ? 'Main Category' : level === 1 ? 'Sub Category' : 'Tertiary Category';
    const selectedIdInLevel = this.selectedCategoryPath[level]?.id;

    return html`
      <div id="level-${level}" class="shrink-0 w-60 p-2 bg-gray-100 dark:bg-gray-700 rounded-xl">
        <h3 class="font-semibold text-gray-700 dark:text-gray-300 mb-3">${levelTitle}</h3>
        <div id="category-list-${level}" class="flex flex-col space-y-2">
          ${categories.map(
            (category) => html`
              <div
                class="category-item p-2 rounded-lg border border-gray-300 dark:border-gray-600 text-left font-medium flex items-center justify-between transition-colors bg-white dark:bg-gray-800 hover:bg-gray-100 dark:hover:bg-gray-700 ${selectedIdInLevel ===
                category.id
                  ? 'active'
                  : ''}"
                @click=${() => this.handleCategoryClick(category, level)}
              >
                <span>${category.name}</span>
                ${category.subCategories && category.subCategories.length > 0
                  ? html`<span class="ml-2 text-gray-400 dark:text-gray-500 font-bold">›</span>`
                  : ''}
              </div>
            `,
          )}
        </div>
      </div>
    `;
  }

  render() {
    return html`
      <div class="flex flex-col h-screen">
        <app-navbar activePage="search"></app-navbar>
        <div
          class="p-2 bg-slate-50 dark:bg-gray-900 text-gray-800 dark:text-gray-200 flex flex-col items-center overflow-y-auto"
          @scroll=${(e: Event) => scrollableOnScroll(e.currentTarget as HTMLElement)}
        >
          <div class="bg-white dark:bg-gray-800 p-1 sm:p-6 rounded-2xl shadow-xl w-full max-w-6xl">
            <form @submit=${this.handleSearch} class="mb-2">
              <div class="join w-full">
                <input
                  name="search-query"
                  class="input input-bordered join-item w-full"
                  placeholder="Search"
                  .value=${this.searchQuery}
                  @input=${this.handleSearchQueryInput}
                />
                <button class="btn join-item btn-primary" type="submit">
                  <span class="icon-[material-symbols--search]" style="width: 1.2em; height: 1.2em;"></span>
                </button>
              </div>
            </form>

            <div class="collapse collapse-arrow mt-2">
              <input
                id="search-collapse"
                type="checkbox"
                .checked=${!this.currentKeyword && !this.currentIndexer && !this.currentCategory}
              />

              <div class="collapse-title p-0 pt-2 pb-2 flex flex-row gap-2">
                <span class="p-2 text-sm">Searching in: </span>
                <div class="breadcrumbs text-sm">
                  <ul>
                    <li>${this.selectedIndexer}</li>
                    ${this.selectedCategoryPath.map((c) => html`<li>${c.name}</li>`)}
                  </ul>
                </div>
              </div>

              <div class="collapse-content p-0 flex flex-row space-x-4">
                <div class="shrink-0 w-60 p-2 bg-gray-100 dark:bg-gray-700 rounded-xl">
                  <h3 class="font-semibold text-gray-700 dark:text-gray-300 mb-3">Indexer</h3>
                  <div class="flex flex-col space-y-2">
                    ${this.indexers.map(
                      (indexer) => html`
                        <div
                          class="category-item p-2 rounded-lg border border-gray-300 dark:border-gray-600 text-left font-medium flex items-center justify-between transition-colors bg-white dark:bg-gray-800 hover:bg-gray-100 dark:hover:bg-gray-700 ${this
                            .selectedIndexer === indexer
                            ? 'active'
                            : ''}"
                          @click=${() => this.handleIndexerChange(indexer)}
                        >
                          <span>${indexer}</span>
                          <span class="ml-2 text-gray-400 dark:text-gray-500 font-bold">›</span>
                        </div>
                      `,
                    )}
                  </div>
                </div>

                <div class="scroll-container overflow-x-auto flex space-x-4 pb-4 grow">
                  ${this.displayedCategoryLevels.map((categories, index) =>
                    this.renderCategoryLevel(categories, index),
                  )}
                </div>
              </div>
            </div>

            ${this.isSearching
              ? html`
                  <div class="flex justify-center items-center py-8">
                    <span class="loading loading-dots loading-lg"></span>
                  </div>
                `
              : ''}
            ${this.currentKeyword || this.currentIndexer || this.currentCategory
              ? html`
                  ${this.currentKeyword
                    ? html`<h2 class="text-xl font-semibold mb-4 text-gray-800 dark:text-gray-200">
                        Result For "${this.currentKeyword}"
                      </h2>`
                    : ''}
                  <resource-list
                    .keyword=${this.currentKeyword}
                    .indexerId=${this.currentIndexer}
                    .category=${this.currentCategory}
                    .page=${this.currentPage}
                  ></resource-list>
                `
              : ''}
          </div>
        </div>
      </div>
    `;
  }
}
