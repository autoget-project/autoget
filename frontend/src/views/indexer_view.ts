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
    if (typeof window !== "undefined" && window.innerWidth < 1024) {
      this.sidebarVisible = false;
    }
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
    const isDesktop = typeof window !== "undefined" && window.innerWidth >= 1024;
    const sidebarDesktopStyle = this.sidebarVisible
      ? `width: ${this.sidebarWidth}px;`
      : "width: 0;";
    const transitionClass = this.isResizing ? "" : "transition-all duration-300 ease-in-out";

    return html`
      <div class="flex flex-col h-screen relative" @sidebar-toggle=${this.handleSidebarToggle}>
        <app-navbar
          .activePage=${this.indexerId}
          .sidebarVisible=${this.sidebarVisible}
        ></app-navbar>

        <!-- Mobile Drawer Overlay Backdrop -->
        ${
          !isDesktop && this.sidebarVisible
            ? html`
                <div
                  class="fixed inset-0 bg-black/50 z-30 lg:hidden"
                  @click=${this.handleSidebarToggle}
                ></div>
              `
            : ""
        }

        <div class="flex flex-row grow overflow-hidden relative">
          <!-- Sidebar: drawer on mobile, static on desktop -->
          <div
            class="bg-base-200 overflow-y-auto overflow-x-hidden ${transitionClass} ${
              this.sidebarVisible ? "" : "opacity-0 pointer-events-none"
            } lg:relative lg:shrink-0 lg:z-auto fixed inset-y-0 left-0 z-40 max-w-[85vw] shadow-2xl lg:shadow-none"
            style=${isDesktop ? sidebarDesktopStyle : this.sidebarVisible ? "width: 280px;" : "width: 0;"}
            id="left-panel-categories"
          >
            <div class="flex items-center justify-between p-3 border-b border-base-300 lg:hidden">
              <span class="font-bold text-sm">Categories</span>
              <button class="btn btn-ghost btn-xs btn-circle" @click=${this.handleSidebarToggle}>
                ✕
              </button>
            </div>
            <ul
              class="menu bg-base-200 rounded-box w-full p-2"
              @click=${() => {
                if (!isDesktop) {
                  this.sidebarVisible = false;
                }
              }}
            >
              ${this.categories.map((category) => this.renderCategory(category))}
            </ul>
          </div>

          <!-- Resize divider handle (desktop only) -->
          <div
            class="hidden lg:block ${this.sidebarVisible ? "w-1.5" : "w-0"} shrink-0 cursor-col-resize select-none ${
              this.isResizing ? "bg-primary" : "hover:bg-primary/50"
            } ${transitionClass}"
            style="touch-action: none;"
            @pointerdown=${this.handleSidebarResizeStart}
          ></div>

          <!-- Main content -->
          <div
            class="grow min-w-0 p-2 sm:p-4 overflow-y-auto"
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
