import { html, LitElement, unsafeCSS, css, type TemplateResult, type PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";

import { type Category, fetchIndexerCategories } from "../utils/api";
import "../components/navbar.ts";
import "../components/resource_list.ts";
import { scrollableOnScroll } from "../utils/scroll_to_top";
import globalStyles from "/src/index.css?inline";

@customElement("indexer-view")
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
  public indexerId: string = "";

  @state()
  private categories: Category[] = [];

  @property({ type: String })
  public category: string = "";

  @property({ type: Number })
  public page: number = 1;

  @state()
  private sidebarVisible: boolean = true;

  @state()
  private sidebarWidth: number = 256;

  @state()
  private isResizing: boolean = false;

  private readonly sidebarWidthStorageKey = "indexer-sidebar-width";

  private readonly sidebarMinWidth = 160;

  private readonly sidebarMaxWidth = 600;

  private handleSidebarToggle(): void {
    this.sidebarVisible = !this.sidebarVisible;
  }

  private clampSidebarWidth(width: number): number {
    return Math.min(this.sidebarMaxWidth, Math.max(this.sidebarMinWidth, Math.round(width)));
  }

  private handleSidebarResizeStart(e: PointerEvent): void {
    if (e.button !== 0) {
      return;
    }
    e.preventDefault();
    const target = e.currentTarget as HTMLElement;
    target.setPointerCapture(e.pointerId);
    this.isResizing = true;

    const onMove = (ev: PointerEvent): void => {
      this.sidebarWidth = this.clampSidebarWidth(ev.clientX);
    };
    const onEnd = (): void => {
      target.removeEventListener("pointermove", onMove);
      target.removeEventListener("pointerup", onEnd);
      target.removeEventListener("pointercancel", onEnd);
      localStorage.setItem(this.sidebarWidthStorageKey, String(this.sidebarWidth));
      this.isResizing = false;
    };
    target.addEventListener("pointermove", onMove);
    target.addEventListener("pointerup", onEnd);
    target.addEventListener("pointercancel", onEnd);
  }

  private renderCategory(category: Category): TemplateResult {
    const isActive = this.category === category.id;
    const activeClass = isActive ? "menu-active" : "";

    if (category.subCategories && category.subCategories.length > 0) {
      return html`
        <li>
          <a class="${activeClass} min-w-0" href="/indexers/${this.indexerId}/${category.id}">
            <span class="truncate">${category.name}</span>
          </a>
          <ul>
            ${category.subCategories.map((child) => this.renderCategory(child))}
          </ul>
        </li>
      `;
    } else {
      return html`<li>
        <a class="${activeClass} min-w-0" href="/indexers/${this.indexerId}/${category.id}">
          <span class="truncate">${category.name}</span>
        </a>
      </li> `;
    }
  }

  async connectedCallback() {
    super.connectedCallback();
    const saved = localStorage.getItem(this.sidebarWidthStorageKey);
    if (saved) {
      const parsed = parseInt(saved, 10);
      if (!Number.isNaN(parsed)) {
        this.sidebarWidth = this.clampSidebarWidth(parsed);
      }
    }
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
    if (changedProperties.has("indexerId")) {
      await this.fetchIndexerCategories();
    }

    if (this.indexerId) {
      let title = "AutoGet - " + this.indexerId;
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
    const sidebarStyle = this.sidebarVisible ? `width: ${this.sidebarWidth}px;` : "width: 0;";
    const transitionClass = this.isResizing ? "" : "transition-all duration-300 ease-in-out";
    return html`
      <div class="flex flex-col h-screen" @sidebar-toggle=${this.handleSidebarToggle}>
        <app-navbar
          .activePage=${this.indexerId}
          .sidebarVisible=${this.sidebarVisible}
        ></app-navbar>

        <div class="flex flex-row grow overflow-hidden">
          <div
            class="bg-base-200 overflow-y-auto overflow-x-hidden shrink-0 ${transitionClass} ${
              this.sidebarVisible ? "" : "opacity-0"
            }"
            style=${sidebarStyle}
            id="left-panel-categories"
          >
            <ul class="menu bg-base-200 rounded-box w-full">
              ${this.categories.map((category) => this.renderCategory(category))}
            </ul>
          </div>

          <div
            class="${this.sidebarVisible ? "w-1.5" : "w-0"} shrink-0 cursor-col-resize select-none ${
              this.isResizing ? "bg-primary" : "hover:bg-primary/50"
            } ${transitionClass}"
            style="touch-action: none;"
            @pointerdown=${this.handleSidebarResizeStart}
          ></div>

          <div
            class="grow min-w-0 p-4 overflow-y-auto"
            id="content"
            @scroll=${(e: Event) => scrollableOnScroll(e.currentTarget as HTMLElement)}
          >
            <resource-list
              .indexerId=${this.indexerId}
              .category=${this.category}
              .page=${this.page}
            ></resource-list>
          </div>
        </div>
      </div>
    `;
  }
}
