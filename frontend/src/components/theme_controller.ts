import { LitElement, html, unsafeCSS } from "lit";
import { customElement, state } from "lit/decorators.js";

import { THEMES, getStoredTheme, applyTheme, type ThemeName } from "../utils/theme.ts";
import globalStyles from "/src/index.css?inline";

@customElement("theme-controller")
export class ThemeController extends LitElement {
  static styles = [unsafeCSS(globalStyles)];

  @state()
  private currentTheme: ThemeName = getStoredTheme();

  private onThemeChanged = (e: Event) => {
    const customEv = e as CustomEvent<{ theme: ThemeName }>;
    if (customEv.detail?.theme) {
      this.currentTheme = customEv.detail.theme;
    }
  };

  connectedCallback() {
    super.connectedCallback();
    this.currentTheme = getStoredTheme();
    window.addEventListener("autoget-theme-changed", this.onThemeChanged);
  }

  disconnectedCallback() {
    window.removeEventListener("autoget-theme-changed", this.onThemeChanged);
    super.disconnectedCallback();
  }

  private handleSelectTheme(theme: ThemeName) {
    applyTheme(theme);
    // Close dropdown on click by blurring active element
    if (this.shadowRoot?.activeElement instanceof HTMLElement) {
      this.shadowRoot.activeElement.blur();
    }
  }

  render() {
    return html`
      <div class="dropdown dropdown-end">
        <div
          tabindex="0"
          role="button"
          class="btn btn-ghost btn-sm gap-1 px-2"
          aria-label="Change theme"
          title="Change theme"
        >
          <span class="icon-[solar--pallete-2-linear] w-5 h-5"></span>
          <span class="icon-[heroicons--chevron-down] w-3 h-3 opacity-60"></span>
        </div>
        <ul
          tabindex="0"
          class="dropdown-content menu bg-base-100 rounded-box z-50 mt-2 w-52 p-2 shadow-xl border border-base-300"
        >
          <li class="menu-title text-xs">Themes</li>
          ${THEMES.map((theme) => {
            const isSelected = this.currentTheme === theme.id;
            return html`
              <li>
                <button
                  type="button"
                  class="flex items-center justify-between py-2 ${isSelected ? "menu-active font-bold" : ""}"
                  @click=${() => this.handleSelectTheme(theme.id)}
                >
                  <span class="flex items-center gap-2">
                    <span
                      class="${theme.isDark ? "icon-[solar--moon-stars-linear]" : "icon-[solar--sun-2-linear]"} w-4 h-4"
                    ></span>
                    <span>${theme.label}</span>
                  </span>
                  ${
                    isSelected
                      ? html`<span
                          class="icon-[heroicons--check-16-solid] w-4 h-4 text-primary"
                        ></span>`
                      : ""
                  }
                </button>
              </li>
            `;
          })}
        </ul>
      </div>
    `;
  }
}
